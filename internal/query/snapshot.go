// Package query runs investigation queries over hot DuckDB rows and the cold
// Parquet files the manifest lists, consistently with a concurrent flush.
package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
)

// Snapshot is a pinned DuckDB read transaction holding one gate slot. Every
// query in a request runs through one Snapshot, which is what keeps a
// concurrent flush from showing an hour twice or not at all: the snapshot
// predates any hot DELETE that follows a manifest insert, and Source filters
// out hot rows the manifest's files already hold.
type Snapshot struct {
	conn     *sql.Conn
	tx       *sql.Tx
	manifest *manifest.Manifest
	dataDir  string
	release  func()
}

// pinSQL makes DuckDB start the transaction's snapshot now: DuckDB assigns a
// transaction's start time on its first read of the database, not at BEGIN.
// Source reads hot before the manifest either way; pinning at Open makes
// every read in the request see the hot tables as they were when it began.
const pinSQL = "SELECT count(*) FROM (SELECT 1 FROM logs LIMIT 1)"

// Open takes a gate slot and a dedicated connection and begins a read
// transaction on it. Callers bound ctx with the request deadline, which
// covers waiting for the slot too, and defer Close.
func Open(ctx context.Context, store *duckdb.Store, m *manifest.Manifest, gate *Gate, dataDir string) (*Snapshot, error) {
	release, err := gate.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for query slot: %w", err)
	}
	conn, err := store.Conn(ctx)
	if err != nil {
		release()
		return nil, fmt.Errorf("query connection: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		release()
		return nil, fmt.Errorf("begin snapshot: %w", err)
	}
	s := &Snapshot{conn: conn, tx: tx, manifest: m, dataDir: dataDir, release: release}
	var n int
	if err := tx.QueryRowContext(ctx, pinSQL).Scan(&n); err != nil {
		s.Close()
		return nil, fmt.Errorf("pin snapshot: %w", err)
	}
	return s, nil
}

// QueryContext runs a query inside the snapshot.
func (s *Snapshot) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.tx.QueryContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query inside the snapshot.
func (s *Snapshot) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.tx.QueryRowContext(ctx, query, args...)
}

// Close ends the transaction, returns the connection, and frees the slot. It
// is safe to call more than once.
func (s *Snapshot) Close() {
	if s.release == nil {
		return
	}
	_ = s.tx.Rollback()
	_ = s.conn.Close()
	s.release()
	s.release = nil
}
