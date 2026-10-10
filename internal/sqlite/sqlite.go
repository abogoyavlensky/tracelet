// Package sqlite owns the application database, tracelet.sqlite: opening it
// with the right pragmas, the embedded migrations, and the stores over it.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens or creates the database at path. The pragmas go into the DSN so
// every pooled connection gets them: WAL so readers do not block the writer,
// a busy timeout instead of immediate SQLITE_BUSY, and enforced foreign keys.
func Open(path string) (*sql.DB, error) {
	// SQLite decodes %XX in URI paths, so escaping keeps '?' and '#' in a
	// directory name from being read as the query or fragment.
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return db, nil
}

const createMigrationsSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
)`

// Migrate applies, in order, every embedded migration not yet recorded in
// schema_migrations, each in its own transaction, and returns the versions it
// applied. A migration file is named NNN_description.sql.
func Migrate(ctx context.Context, db *sql.DB) ([]int, error) {
	if _, err := db.ExecContext(ctx, createMigrationsSQL); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	current, err := Version(ctx, db)
	if err != nil {
		return nil, err
	}

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	slices.Sort(names)

	var applied []int
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return applied, err
		}
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return applied, fmt.Errorf("read %s: %w", name, err)
		}
		if err := apply(ctx, db, version, string(body)); err != nil {
			return applied, fmt.Errorf("migration %s: %w", path.Base(name), err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

// Version returns the highest applied migration version, or 0.
func Version(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRowContext(ctx, "SELECT coalesce(max(version), 0) FROM schema_migrations").Scan(&v); err != nil {
		return 0, fmt.Errorf("schema version: %w", err)
	}
	return v, nil
}

func apply(ctx context.Context, db *sql.DB, version int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		version, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(path.Base(name), "_")
	if !ok {
		return 0, fmt.Errorf("migration %s: name must be NNN_description.sql", name)
	}
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration %s: %w", name, err)
	}
	return v, nil
}
