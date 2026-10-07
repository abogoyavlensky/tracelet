// Package manifest records every cold Parquet file in a SQLite database. The
// manifest, not a directory glob, decides which files a query reads.
package manifest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrDuplicate is returned when a file path is already recorded.
var ErrDuplicate = errors.New("file already in manifest")

// SchemaVersion is the hot/cold table schema the files are written with.
const SchemaVersion = 1

// File is one Parquet file of one signal and hour.
type File struct {
	ID            int64
	Signal        string
	Hour          time.Time
	Path          string // relative to the data dir
	Rows          int64
	MinTS         time.Time
	MaxTS         time.Time
	MaxIngestTS   time.Time // ingest cutoff: hot rows of Hour at or below it are in this file
	Bytes         int64
	SchemaVersion int
	CreatedAt     time.Time
}

// Manifest is the SQLite file manifest.
type Manifest struct {
	db *sql.DB
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS files (
  id INTEGER PRIMARY KEY,
  signal TEXT NOT NULL,
  hour TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  rows INTEGER NOT NULL,
  min_ts TEXT NOT NULL, max_ts TEXT NOT NULL,
  max_ingest_ts TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS files_signal_hour ON files (signal, hour);`

// Timestamps are fixed-width UTC strings so that SQL string comparison
// orders them correctly.
const (
	tsLayout   = "2006-01-02T15:04:05.000000Z"
	hourLayout = "2006-01-02T15:04:05Z"
)

func formatTS(t time.Time) string   { return t.UTC().Format(tsLayout) }
func formatHour(t time.Time) string { return t.UTC().Format(hourLayout) }

// Open opens or creates the manifest database at path.
func Open(path string) (*Manifest, error) {
	// SQLite decodes %XX in URI paths, so escaping keeps '?' and '#' in a
	// directory name from being read as the query or fragment.
	dsn := "file:" + url.PathEscape(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create manifest schema: %w", err)
	}
	return &Manifest{db: db}, nil
}

// DB returns the underlying database, for VACUUM INTO and similar.
func (m *Manifest) DB() *sql.DB { return m.db }

// Close closes the database.
func (m *Manifest) Close() error { return m.db.Close() }

const addSQL = `INSERT INTO files
  (signal, hour, path, rows, min_ts, max_ts, max_ingest_ts, bytes, schema_version, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Add records a file. It returns ErrDuplicate if the path is already there.
func (m *Manifest) Add(ctx context.Context, f File) error {
	created := f.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	schema := f.SchemaVersion
	if schema == 0 {
		schema = SchemaVersion
	}
	_, err := m.db.ExecContext(ctx, addSQL,
		f.Signal, formatHour(f.Hour), f.Path, f.Rows,
		formatTS(f.MinTS), formatTS(f.MaxTS), formatTS(f.MaxIngestTS),
		f.Bytes, schema, formatTS(created))
	if sqliteErr, ok := errors.AsType[*sqlite.Error](err); ok && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return fmt.Errorf("add %s: %w", f.Path, ErrDuplicate)
	}
	if err != nil {
		return fmt.Errorf("add %s: %w", f.Path, err)
	}
	return nil
}

// Remove deletes a file's row. Removing a path that is not there is not an
// error.
func (m *Manifest) Remove(ctx context.Context, path string) error {
	if _, err := m.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

const selectFiles = `SELECT id, signal, hour, path, rows, min_ts, max_ts, max_ingest_ts, bytes, schema_version, created_at
  FROM files`

// FilesFor returns a signal's files whose rows overlap [from, to), by the
// rows' real time range rather than the hour, so late-data files are found.
func (m *Manifest) FilesFor(ctx context.Context, signal string, from, to time.Time) ([]File, error) {
	return m.query(ctx, selectFiles+" WHERE signal = ? AND min_ts < ? AND max_ts >= ? ORDER BY min_ts, path",
		signal, formatTS(to), formatTS(from))
}

// All returns every file, ordered by signal, hour, and path.
func (m *Manifest) All(ctx context.Context) ([]File, error) {
	return m.query(ctx, selectFiles+" ORDER BY signal, hour, path")
}

// HourFiles returns every file of one hour across signals.
func (m *Manifest) HourFiles(ctx context.Context, hour time.Time) ([]File, error) {
	return m.query(ctx, selectFiles+" WHERE hour = ? ORDER BY signal, path", formatHour(hour))
}

func (m *Manifest) query(ctx context.Context, q string, args ...any) ([]File, error) {
	rows, err := m.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var hour, minTS, maxTS, maxIngest, created string
		err := rows.Scan(&f.ID, &f.Signal, &hour, &f.Path, &f.Rows, &minTS, &maxTS, &maxIngest,
			&f.Bytes, &f.SchemaVersion, &created)
		if err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		if f.Hour, err = time.Parse(hourLayout, hour); err != nil {
			return nil, fmt.Errorf("parse hour: %w", err)
		}
		for _, p := range []struct {
			dst *time.Time
			src string
		}{{&f.MinTS, minTS}, {&f.MaxTS, maxTS}, {&f.MaxIngestTS, maxIngest}, {&f.CreatedAt, created}} {
			if *p.dst, err = time.Parse(tsLayout, p.src); err != nil {
				return nil, fmt.Errorf("parse timestamp: %w", err)
			}
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// Hours returns the distinct hours a signal has files for, oldest first.
func (m *Manifest) Hours(ctx context.Context, signal string) ([]time.Time, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT DISTINCT hour FROM files WHERE signal = ? ORDER BY hour", signal)
	if err != nil {
		return nil, fmt.Errorf("query hours: %w", err)
	}
	defer rows.Close()

	var hours []time.Time
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan hour: %w", err)
		}
		h, err := time.Parse(hourLayout, s)
		if err != nil {
			return nil, fmt.Errorf("parse hour: %w", err)
		}
		hours = append(hours, h)
	}
	return hours, rows.Err()
}

// TotalBytes sums the size of every recorded file.
func (m *Manifest) TotalBytes(ctx context.Context) (int64, error) {
	var n int64
	if err := m.db.QueryRowContext(ctx, "SELECT coalesce(sum(bytes), 0) FROM files").Scan(&n); err != nil {
		return 0, fmt.Errorf("total bytes: %w", err)
	}
	return n, nil
}

// OldestHour returns the earliest hour across signals and the total bytes of
// its files. ok is false when the manifest is empty.
func (m *Manifest) OldestHour(ctx context.Context) (hour time.Time, bytes int64, ok bool, err error) {
	var s string
	err = m.db.QueryRowContext(ctx,
		"SELECT hour, sum(bytes) FROM files GROUP BY hour ORDER BY hour LIMIT 1").Scan(&s, &bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, 0, false, nil
	}
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("oldest hour: %w", err)
	}
	hour, err = time.Parse(hourLayout, s)
	if err != nil {
		return time.Time{}, 0, false, fmt.Errorf("parse hour: %w", err)
	}
	return hour, bytes, true, nil
}

// MaxIngestCutoff returns the greatest ingest cutoff over a signal's files
// for one hour: every hot row of that hour stamped at or below it is in a
// file. ok is false when the hour has no files.
func (m *Manifest) MaxIngestCutoff(ctx context.Context, signal string, hour time.Time) (time.Time, bool, error) {
	var s sql.NullString
	err := m.db.QueryRowContext(ctx,
		"SELECT max(max_ingest_ts) FROM files WHERE signal = ? AND hour = ?", signal, formatHour(hour)).Scan(&s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("max ingest cutoff: %w", err)
	}
	if !s.Valid {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(tsLayout, s.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse cutoff: %w", err)
	}
	return t, true, nil
}
