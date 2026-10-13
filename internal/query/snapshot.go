// Package query runs investigation queries over hot DuckDB rows and the cold
// Parquet files the manifest lists, consistently with a concurrent flush.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	coldMax  time.Time // manifest's greatest ingest cutoff, read before the pin
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
	// Before the pin, on purpose: see Watermark.
	coldMax, err := m.MaxIngestTS(ctx)
	if err != nil {
		_ = conn.Close()
		release()
		return nil, fmt.Errorf("read ingest watermark: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		release()
		return nil, fmt.Errorf("begin snapshot: %w", err)
	}
	s := &Snapshot{conn: conn, tx: tx, manifest: m, dataDir: dataDir, release: release, coldMax: coldMax}
	var n int
	if err := tx.QueryRowContext(ctx, pinSQL).Scan(&n); err != nil {
		s.Close()
		return nil, fmt.Errorf("pin snapshot: %w", err)
	}
	return s, nil
}

// Watermark returns an ingest stamp W such that every row of the signal
// stamped at or below W is visible to this snapshot's queries, and every row
// stamped above it is either invisible or may be seen again by a later one.
// It is the greater of two values, each bounded by the pin:
//
//   - the hot maximum, read on the pinned transaction. The single writer
//     commits batches in stamp order, so the hot rows the snapshot sees are a
//     gap-free prefix of the stamps and everything at or below their maximum
//     is visible;
//   - the cold maximum, the manifest's greatest cutoff read in Open before the
//     pin. Every file it counts existed before the pin, and retention cannot
//     unlink one while the snapshot holds its gate slot, so Source lists it.
//
// The zero time means nothing is stored. Two other watermarks are wrong. The
// maximum over Source's rows can skip: its cold half may include a late hour
// flushed after the pin, with a higher stamp than a row of the current hour
// that hot does not show. The maximum over a page of results is ordered by ts,
// not by stamp. And reading the manifest after the pin can skip a row that
// was committed after the pin and hidden behind a later-stamped row that was
// flushed since: the manifest covers the later stamp, so it also covers the
// invisible one.
func (s *Snapshot) Watermark(ctx context.Context, signal string) (time.Time, error) {
	if err := duckdb.CheckSignal(signal); err != nil {
		return time.Time{}, err
	}
	var hot sql.NullTime
	q := fmt.Sprintf("SELECT max(ingest_ts) FROM %s", signal)
	if err := s.tx.QueryRowContext(ctx, q).Scan(&hot); err != nil {
		return time.Time{}, fmt.Errorf("read %s watermark: %w", signal, err)
	}
	w := s.coldMax
	if hot.Valid && hot.Time.After(w) {
		w = hot.Time.UTC()
	}
	return w, nil
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

// Opener opens snapshots over one store, manifest, and gate; it is what
// handlers hold.
type Opener struct {
	Store    *duckdb.Store
	Manifest *manifest.Manifest
	Gate     *Gate
	DataDir  string
}

// Open opens a snapshot; see the package-level Open.
func (o Opener) Open(ctx context.Context) (*Snapshot, error) {
	return Open(ctx, o.Store, o.Manifest, o.Gate, o.DataDir)
}
