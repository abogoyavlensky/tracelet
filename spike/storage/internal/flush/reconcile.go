package flush

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// ReconcileReport counts what Reconcile repaired.
type ReconcileReport struct {
	TmpRemoved     int   `json:"tmp_removed"`
	Adopted        int   `json:"adopted"`
	Dropped        int   `json:"dropped"`
	HotRowsDeleted int64 `json:"hot_rows_deleted"`
}

// Reconcile brings the hot tables, the manifest, and the files on disk back
// into agreement after a crash at any flush or retention step:
//
//  1. delete *.parquet.tmp files, which were never published;
//  2. adopt final-named files missing from the manifest, reading their row
//     count, time range, and ingest cutoff from the file;
//  3. drop manifest rows whose file is missing;
//  4. for every recorded (signal, hour), delete hot rows at or below the
//     hour's greatest cutoff. Rows stamped later are not in any file and stay.
//
// It applies no retention; run retention right after it.
func (f *Flusher) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var rep ReconcileReport

	onDisk, tmps, err := scanTelemetry(f.DataDir)
	if err != nil {
		return rep, err
	}
	for _, tmp := range tmps {
		if err := os.Remove(filepath.Join(f.DataDir, tmp)); err != nil {
			return rep, fmt.Errorf("remove %s: %w", tmp, err)
		}
		rep.TmpRemoved++
	}

	files, err := f.Manifest.All(ctx)
	if err != nil {
		return rep, err
	}
	recorded := map[string]bool{}
	for _, file := range files {
		recorded[file.Path] = true
	}

	for _, rel := range onDisk {
		if recorded[rel] {
			continue
		}
		signal, hour, err := parseFilePath(rel)
		if err != nil {
			return rep, err
		}
		file, err := describeFile(ctx, f.Store.DB(), filepath.Join(f.DataDir, rel))
		if err != nil {
			return rep, err
		}
		file.Signal, file.Hour, file.Path = signal, hour, rel
		if err := f.Manifest.Add(ctx, file); err != nil {
			return rep, err
		}
		rep.Adopted++
	}

	present := map[string]bool{}
	for _, rel := range onDisk {
		present[rel] = true
	}
	for _, file := range files {
		if present[file.Path] {
			continue
		}
		if err := f.Manifest.Remove(ctx, file.Path); err != nil {
			return rep, err
		}
		rep.Dropped++
	}

	for _, signal := range store.Signals {
		hours, err := f.Manifest.Hours(ctx, signal)
		if err != nil {
			return rep, err
		}
		for _, hour := range hours {
			cutoff, ok, err := f.Manifest.MaxIngestCutoff(ctx, signal, hour)
			if err != nil {
				return rep, err
			}
			if !ok {
				continue
			}
			n, err := deleteHot(ctx, f.Store.DB(), signal, hour, cutoff)
			if err != nil {
				return rep, err
			}
			rep.HotRowsDeleted += n
		}
	}
	return rep, nil
}

// scanTelemetry lists final-named Parquet files and temporary files under the
// telemetry directory, as slash-separated paths relative to the data dir.
func scanTelemetry(dataDir string) (files, tmps []string, err error) {
	root := filepath.Join(dataDir, TelemetryDir)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasSuffix(rel, ".parquet.tmp"):
			tmps = append(tmps, rel)
		case strings.HasSuffix(rel, ".parquet"):
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan telemetry: %w", err)
	}
	return files, tmps, nil
}

// parseFilePath reads the signal and hour from
// telemetry/<signal>/date=YYYY-MM-DD/hour=HH/<name>.parquet.
func parseFilePath(rel string) (string, time.Time, error) {
	parts := strings.Split(rel, "/")
	if len(parts) != 5 || parts[0] != TelemetryDir {
		return "", time.Time{}, fmt.Errorf("unexpected file path %s", rel)
	}
	signal := parts[1]
	if err := store.CheckSignal(signal); err != nil {
		return "", time.Time{}, fmt.Errorf("file %s: %w", rel, err)
	}
	date, okDate := strings.CutPrefix(parts[2], "date=")
	hh, okHour := strings.CutPrefix(parts[3], "hour=")
	if !okDate || !okHour {
		return "", time.Time{}, fmt.Errorf("unexpected file path %s", rel)
	}
	hour, err := time.Parse("2006-01-02 15", date+" "+hh)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("file %s: %w", rel, err)
	}
	return signal, hour.UTC(), nil
}

// SignalCounts is one signal's row accounting across hot and cold.
type SignalCounts struct {
	Hot          int64 `json:"hot"`
	Cold         int64 `json:"cold"`
	Total        int64 `json:"total"`
	DistinctKeys int64 `json:"distinct_keys"`
}

// VerifyReport is the invariant check: per-signal counts, and whether the
// manifest and the files on disk agree one to one.
type VerifyReport struct {
	Signals    map[string]SignalCounts `json:"signals"`
	PathsMatch bool                    `json:"paths_match"`
	Missing    []string                `json:"missing,omitempty"`    // recorded, not on disk
	Unrecorded []string                `json:"unrecorded,omitempty"` // on disk, not recorded
}

// rowKeys is the expression that identifies a row uniquely across a run.
var rowKeys = map[string]string{
	"logs":          "json_extract(attributes, '$.seq')",
	"spans":         "(trace_id, span_id)",
	"metric_points": "(series_hash, ts)",
}

// Verify counts every signal's rows in hot and cold and their distinct keys,
// and compares the manifest with the files on disk. It reads every cold row,
// so it is meant for the crash matrix and tests, not the 7-day dataset.
func (f *Flusher) Verify(ctx context.Context) (VerifyReport, error) {
	rep := VerifyReport{Signals: map[string]SignalCounts{}}

	files, err := f.Manifest.All(ctx)
	if err != nil {
		return rep, err
	}
	bySignal := map[string][]string{}
	var recorded []string
	for _, file := range files {
		bySignal[file.Signal] = append(bySignal[file.Signal], filepath.Join(f.DataDir, file.Path))
		recorded = append(recorded, file.Path)
	}

	for _, signal := range store.Signals {
		cold := "SELECT * FROM " + signal + " WHERE false"
		if paths := bySignal[signal]; len(paths) > 0 {
			cold = "SELECT * FROM " + ParquetSource(paths)
		}
		q := fmt.Sprintf(`WITH hot AS (SELECT * FROM %[1]s), cold AS (%[2]s),
			everything AS (SELECT %[3]s AS k FROM hot UNION ALL SELECT %[3]s AS k FROM cold)
			SELECT (SELECT count(*) FROM hot), (SELECT count(*) FROM cold),
			       count(*), count(DISTINCT k) FROM everything`,
			signal, cold, rowKeys[signal])
		var c SignalCounts
		if err := f.Store.DB().QueryRowContext(ctx, q).Scan(&c.Hot, &c.Cold, &c.Total, &c.DistinctKeys); err != nil {
			return rep, fmt.Errorf("verify %s: %w", signal, err)
		}
		rep.Signals[signal] = c
	}

	onDisk, _, err := scanTelemetry(f.DataDir)
	if err != nil {
		return rep, err
	}
	rep.Missing = difference(recorded, onDisk)
	rep.Unrecorded = difference(onDisk, recorded)
	rep.PathsMatch = len(rep.Missing) == 0 && len(rep.Unrecorded) == 0
	return rep, nil
}

// ParquetSource returns a read_parquet call over the given server-generated
// paths.
func ParquetSource(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = quote(p)
	}
	return "read_parquet([" + strings.Join(quoted, ", ") + "], union_by_name = true)"
}

// difference returns the elements of a not in b, sorted.
func difference(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}
