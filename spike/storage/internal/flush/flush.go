// Package flush moves closed hours from the hot DuckDB tables into Parquet
// files recorded in the manifest, reconciles the two after a crash, and
// enforces retention.
package flush

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// Hook steps, in the order a flush reaches them.
const (
	StepWritten  = "written"
	StepRenamed  = "renamed"
	StepRecorded = "recorded"
	StepDeleted  = "deleted"
)

// HotWindow is how far behind now an hour must start to be flushed: the
// current and the previous hour stay hot.
const HotWindow = 2 * time.Hour

// Flusher moves hot rows into Parquet files.
type Flusher struct {
	Store    *store.Store
	Manifest *manifest.Manifest
	DataDir  string
	// Hook, when set, runs after each step; an error aborts the flush there.
	// Tests use it to simulate a crash at each step.
	Hook func(step string) error
}

// Stats describes one flushed (signal, hour).
type Stats struct {
	Signal         string        `json:"signal"`
	Hour           time.Time     `json:"hour"`
	Path           string        `json:"path"`
	Rows           int64         `json:"rows"`
	Bytes          int64         `json:"bytes"`
	Duration       time.Duration `json:"duration"`
	DeleteDuration time.Duration `json:"delete_duration"`
}

type signalHour struct {
	signal string
	hour   time.Time
}

// FlushDue flushes every hot hour H with H + HotWindow <= now, oldest first.
func (f *Flusher) FlushDue(ctx context.Context, now time.Time) ([]Stats, error) {
	return f.flushWhere(ctx, func(h time.Time) bool { return !h.Add(HotWindow).After(now) })
}

// FlushAll flushes every hot hour, including the current one, leaving the
// hot tables empty of rows committed before the call.
func (f *Flusher) FlushAll(ctx context.Context) ([]Stats, error) {
	return f.flushWhere(ctx, func(time.Time) bool { return true })
}

func (f *Flusher) flushWhere(ctx context.Context, due func(time.Time) bool) ([]Stats, error) {
	var todo []signalHour
	for _, signal := range store.Signals {
		hours, err := f.Store.HotHours(ctx, signal)
		if err != nil {
			return nil, err
		}
		for _, h := range hours {
			if due(h) {
				todo = append(todo, signalHour{signal, h})
			}
		}
	}
	slices.SortStableFunc(todo, func(a, b signalHour) int { return a.hour.Compare(b.hour) })

	var all []Stats
	for _, sh := range todo {
		st, ok, err := f.flushHour(ctx, sh.signal, sh.hour)
		if err != nil {
			return all, err
		}
		if ok {
			all = append(all, st)
		}
	}
	return all, nil
}

// flushHour runs the four steps for one (signal, hour). ok is false when the
// hour turned out to have no rows.
func (f *Flusher) flushHour(ctx context.Context, signal string, hour time.Time) (Stats, bool, error) {
	start := time.Now()
	st := Stats{Signal: signal, Hour: hour}

	relDir := HourDir(signal, hour)
	dir := filepath.Join(f.DataDir, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return st, false, fmt.Errorf("create %s: %w", relDir, err)
	}
	name, err := fileName()
	if err != nil {
		return st, false, err
	}
	st.Path = filepath.ToSlash(filepath.Join(relDir, name))
	final := filepath.Join(f.DataDir, st.Path)
	tmp := final + ".tmp"

	// 0. Finish any earlier flush of this hour that was recorded but whose
	// delete did not run, so its rows are not exported a second time.
	prev, recorded, err := f.Manifest.MaxIngestCutoff(ctx, signal, hour)
	if err != nil {
		return st, false, err
	}
	if recorded {
		if _, err := deleteHot(ctx, f.Store.DB(), signal, hour, prev); err != nil {
			return st, false, err
		}
	}

	// 1. Capture the cutoff and export the rows at or below it in one snapshot.
	cutoff, ok, err := f.export(ctx, signal, hour, tmp)
	if err != nil || !ok {
		return st, false, err
	}
	if err := f.hook(StepWritten); err != nil {
		return st, false, fmt.Errorf("flush %s %s: %w", signal, hour.Format(time.RFC3339), err)
	}

	// 2. Describe the file from the file itself, make it durable, and publish it.
	file, err := describeFile(ctx, f.Store.DB(), tmp)
	if err != nil {
		return st, false, err
	}
	if err := syncFile(tmp); err != nil {
		return st, false, err
	}
	if err := syncDirs(f.DataDir, relDir); err != nil {
		return st, false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return st, false, fmt.Errorf("rename %s: %w", st.Path, err)
	}
	if err := syncDir(dir); err != nil {
		return st, false, err
	}
	if err := f.hook(StepRenamed); err != nil {
		return st, false, fmt.Errorf("flush %s %s: %w", signal, hour.Format(time.RFC3339), err)
	}

	// 3. Record it. The file's own max(ingest_ts) equals the cutoff.
	file.Signal, file.Hour, file.Path = signal, hour, st.Path
	file.MaxIngestTS = cutoff
	if err := f.Manifest.Add(ctx, file); err != nil {
		return st, false, err
	}
	if err := f.hook(StepRecorded); err != nil {
		return st, false, fmt.Errorf("flush %s %s: %w", signal, hour.Format(time.RFC3339), err)
	}

	// 4. Drop exactly the exported rows from hot.
	delStart := time.Now()
	if _, err := deleteHot(ctx, f.Store.DB(), signal, hour, cutoff); err != nil {
		return st, false, err
	}
	st.DeleteDuration = time.Since(delStart)
	if err := f.hook(StepDeleted); err != nil {
		return st, false, fmt.Errorf("flush %s %s: %w", signal, hour.Format(time.RFC3339), err)
	}

	st.Rows, st.Bytes = file.Rows, file.Bytes
	st.Duration = time.Since(start)
	return st, true, nil
}

func (f *Flusher) hook(step string) error {
	if f.Hook == nil {
		return nil
	}
	if err := f.Hook(step); err != nil {
		return fmt.Errorf("hook at %s: %w", step, err)
	}
	return nil
}

// export writes the hour's rows stamped at or below the hour's current
// max(ingest_ts) to tmp, reading both in one transaction so the cutoff and
// the copied rows come from the same snapshot.
func (f *Flusher) export(ctx context.Context, signal string, hour time.Time, tmp string) (time.Time, bool, error) {
	conn, err := f.Store.Conn(ctx)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("flush connection: %w", err)
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("begin export: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cutoff sql.NullTime
	q := fmt.Sprintf("SELECT max(ingest_ts) FROM %s WHERE ts >= ? AND ts < ?", signal)
	if err := tx.QueryRowContext(ctx, q, hour, hour.Add(time.Hour)).Scan(&cutoff); err != nil {
		return time.Time{}, false, fmt.Errorf("cutoff %s: %w", signal, err)
	}
	if !cutoff.Valid {
		return time.Time{}, false, nil
	}

	copySQL := fmt.Sprintf(
		"COPY (SELECT * FROM %s WHERE ts >= %s AND ts < %s AND ingest_ts <= %s ORDER BY ts) TO %s (FORMAT parquet, COMPRESSION zstd)",
		signal, tsLiteral(hour), tsLiteral(hour.Add(time.Hour)), tsLiteral(cutoff.Time), quote(tmp))
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return time.Time{}, false, fmt.Errorf("copy %s: %w", signal, err)
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, false, fmt.Errorf("commit export: %w", err)
	}
	return cutoff.Time.UTC(), true, nil
}

func deleteHot(ctx context.Context, db *sql.DB, signal string, hour, cutoff time.Time) (int64, error) {
	q := fmt.Sprintf("DELETE FROM %s WHERE ts >= ? AND ts < ? AND ingest_ts <= ?", signal)
	res, err := db.ExecContext(ctx, q, hour, hour.Add(time.Hour), cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete hot %s: %w", signal, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete hot %s: %w", signal, err)
	}
	return n, nil
}

// describeFile reads a Parquet file's row count, time range, ingest cutoff,
// and size, so the manifest reflects the file rather than the hot table.
func describeFile(ctx context.Context, db *sql.DB, path string) (manifest.File, error) {
	var f manifest.File
	q := fmt.Sprintf("SELECT sum(num_rows) FROM parquet_file_metadata(%s)", quote(path))
	if err := db.QueryRowContext(ctx, q).Scan(&f.Rows); err != nil {
		return f, fmt.Errorf("parquet metadata %s: %w", path, err)
	}
	q = fmt.Sprintf("SELECT min(ts), max(ts), max(ingest_ts) FROM read_parquet(%s)", quote(path))
	if err := db.QueryRowContext(ctx, q).Scan(&f.MinTS, &f.MaxTS, &f.MaxIngestTS); err != nil {
		return f, fmt.Errorf("parquet range %s: %w", path, err)
	}
	f.MinTS, f.MaxTS, f.MaxIngestTS = f.MinTS.UTC(), f.MaxTS.UTC(), f.MaxIngestTS.UTC()
	info, err := os.Stat(path)
	if err != nil {
		return f, fmt.Errorf("stat %s: %w", path, err)
	}
	f.Bytes = info.Size()
	return f, nil
}

// TelemetryDir is the cold-file root, relative to the data dir.
const TelemetryDir = "telemetry"

// HourDir returns the directory, relative to the data dir, of one signal's
// files for one hour.
func HourDir(signal string, hour time.Time) string {
	hour = hour.UTC()
	return filepath.Join(TelemetryDir, signal, "date="+hour.Format("2006-01-02"), "hour="+hour.Format("15"))
}

func fileName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random file name: %w", err)
	}
	return fmt.Sprintf("%d-%s.parquet", time.Now().UnixNano(), hex.EncodeToString(b)), nil
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open for sync %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	return f.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir %s: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("sync dir %s: %w", dir, err)
	}
	return d.Close()
}

// syncDirs fsyncs every directory from rel up to and including the data dir,
// so freshly created hour and date directories survive a crash too.
func syncDirs(dataDir, rel string) error {
	var errs []error
	for d := rel; d != "." && d != string(filepath.Separator); d = filepath.Dir(d) {
		errs = append(errs, syncDir(filepath.Join(dataDir, d)))
	}
	errs = append(errs, syncDir(dataDir))
	return errors.Join(errs...)
}

func tsLiteral(t time.Time) string {
	return "TIMESTAMP '" + t.UTC().Format("2006-01-02 15:04:05.999999") + "'"
}

// quote single-quotes a server-generated path for interpolation into SQL.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
