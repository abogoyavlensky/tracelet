package flush

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
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

	for _, signal := range duckdb.Signals {
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
	if err := duckdb.CheckSignal(signal); err != nil {
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
