// Package store owns the hot DuckDB database: connection limits, the schema of
// the three hot tables, and the batch writer.
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
)

// Signals lists the hot tables, one per telemetry signal, in a fixed order.
var Signals = []string{"logs", "spans", "metric_points"}

// Limits caps DuckDB's resource use.
type Limits struct {
	MemoryLimit string // DuckDB size string, e.g. "256MB"
	Threads     int
	// AllocatorFlushThreshold, when set, is DuckDB's allocator_flush_threshold
	// size string; empty keeps DuckDB's default.
	AllocatorFlushThreshold string
	// AllocatorBackgroundThreads turns on allocator_background_threads.
	AllocatorBackgroundThreads bool
}

// Store is the hot DuckDB database under a data directory.
type Store struct {
	db        *sql.DB
	connector *duckdb.Connector
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS logs (
  ts TIMESTAMP NOT NULL, observed_ts TIMESTAMP, ingest_ts TIMESTAMP NOT NULL,
  project VARCHAR NOT NULL, service VARCHAR NOT NULL, environment VARCHAR, version VARCHAR,
  severity_number SMALLINT, severity_text VARCHAR, body VARCHAR,
  trace_id VARCHAR, span_id VARCHAR, scope VARCHAR,
  resource VARCHAR, attributes VARCHAR
);
CREATE TABLE IF NOT EXISTS spans (
  ts TIMESTAMP NOT NULL, end_ts TIMESTAMP NOT NULL, ingest_ts TIMESTAMP NOT NULL,
  project VARCHAR NOT NULL, service VARCHAR NOT NULL, environment VARCHAR, version VARCHAR,
  trace_id VARCHAR NOT NULL, span_id VARCHAR NOT NULL, parent_span_id VARCHAR,
  name VARCHAR NOT NULL, kind SMALLINT, status_code SMALLINT, status_message VARCHAR,
  duration_ns BIGINT NOT NULL, scope VARCHAR,
  resource VARCHAR, attributes VARCHAR, events VARCHAR, links VARCHAR
);
CREATE TABLE IF NOT EXISTS metric_points (
  ts TIMESTAMP NOT NULL, start_ts TIMESTAMP, ingest_ts TIMESTAMP NOT NULL,
  project VARCHAR NOT NULL, service VARCHAR NOT NULL, environment VARCHAR, version VARCHAR,
  name VARCHAR NOT NULL, unit VARCHAR, type SMALLINT NOT NULL, temporality SMALLINT,
  monotonic BOOLEAN, series_hash UBIGINT NOT NULL,
  resource VARCHAR, attributes VARCHAR,
  value_int BIGINT, value_double DOUBLE,
  count UBIGINT, sum DOUBLE, min DOUBLE, max DOUBLE,
  bucket_counts UBIGINT[], bucket_bounds DOUBLE[]
);`

// Open opens or creates <dataDir>/hot.duckdb with the given limits and makes
// sure the hot tables exist.
func Open(dataDir string, limits Limits) (*Store, error) {
	tmpDir := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Settings are global, so re-applying them on every new pooled connection
	// is harmless and keeps the first connection from racing the rest.
	boot := []string{
		fmt.Sprintf("SET GLOBAL memory_limit = '%s'", limits.MemoryLimit),
		fmt.Sprintf("SET GLOBAL threads = %d", limits.Threads),
		fmt.Sprintf("SET GLOBAL temp_directory = '%s'", strings.ReplaceAll(tmpDir, "'", "''")),
		"SET GLOBAL preserve_insertion_order = false",
		// Hot timestamps are naive UTC; pin the zone so any TIMESTAMPTZ
		// parameter casts the same way on every host.
		"SET GLOBAL TimeZone = 'UTC'",
	}
	if limits.AllocatorFlushThreshold != "" {
		boot = append(boot, fmt.Sprintf("SET GLOBAL allocator_flush_threshold = '%s'",
			strings.ReplaceAll(limits.AllocatorFlushThreshold, "'", "''")))
	}
	if limits.AllocatorBackgroundThreads {
		boot = append(boot, "SET GLOBAL allocator_background_threads = true")
	}
	connector, err := duckdb.NewConnector(filepath.Join(dataDir, "hot.duckdb"), func(execer driver.ExecerContext) error {
		for _, q := range boot {
			if _, err := execer.ExecContext(context.Background(), q, nil); err != nil {
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}

	db := sql.OpenDB(connector)
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		_ = connector.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db, connector: connector}, nil
}

// DB returns the connection pool for queries.
func (s *Store) DB() *sql.DB { return s.db }

// Conn returns a dedicated connection, needed for appenders and transactions
// that must stay on one connection.
func (s *Store) Conn(ctx context.Context) (*sql.Conn, error) { return s.db.Conn(ctx) }

// HotHours returns the distinct UTC hours that have rows in a signal's hot
// table, oldest first.
func (s *Store) HotHours(ctx context.Context, signal string) ([]time.Time, error) {
	if err := CheckSignal(signal); err != nil {
		return nil, err
	}
	q := fmt.Sprintf("SELECT DISTINCT date_trunc('hour', ts) AS h FROM %s ORDER BY h", signal)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("hot hours %s: %w", signal, err)
	}
	defer rows.Close()

	var hours []time.Time
	for rows.Next() {
		var h time.Time
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("scan hot hour: %w", err)
		}
		hours = append(hours, h.UTC())
	}
	return hours, rows.Err()
}

// CheckSignal rejects anything but a known signal name, since signal names
// are interpolated into SQL as table names.
func CheckSignal(signal string) error {
	for _, s := range Signals {
		if s == signal {
			return nil
		}
	}
	return fmt.Errorf("unknown signal %q", signal)
}

// Close closes the pool and the database.
func (s *Store) Close() error {
	dbErr := s.db.Close()
	connErr := s.connector.Close()
	if dbErr != nil {
		return dbErr
	}
	return connErr
}

// Memory is DuckDB's own accounting of its memory, from duckdb_memory().
type Memory struct {
	ByTag map[string]int64 // memory_usage_bytes by tag, non-zero tags only
	Temp  int64            // temporary_storage_bytes over all tags
}

// queryer is satisfied by *sql.DB and *sql.Conn.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// MemoryByTag reads duckdb_memory() through q, which may be a dedicated
// connection so a sampler does not compete with the pool.
func MemoryByTag(ctx context.Context, q queryer) (Memory, error) {
	rows, err := q.QueryContext(ctx, "SELECT tag, memory_usage_bytes, temporary_storage_bytes FROM duckdb_memory()")
	if err != nil {
		return Memory{}, fmt.Errorf("duckdb memory: %w", err)
	}
	defer rows.Close()

	mem := Memory{ByTag: map[string]int64{}}
	for rows.Next() {
		var tag string
		var used, temp int64
		if err := rows.Scan(&tag, &used, &temp); err != nil {
			return Memory{}, fmt.Errorf("scan duckdb memory: %w", err)
		}
		if used > 0 {
			mem.ByTag[tag] = used
		}
		mem.Temp += temp
	}
	if err := rows.Err(); err != nil {
		return Memory{}, fmt.Errorf("read duckdb memory: %w", err)
	}
	return mem, nil
}
