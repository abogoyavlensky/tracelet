// Package query builds the investigation queries over hot rows and the cold
// Parquet files the manifest lists for a time range.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// Column lists in schema order. The explicit list keeps the hot/cold union
// aligned if a column is added later.
const (
	logColumns = `ts, observed_ts, ingest_ts, project, service, environment, version,
  severity_number, severity_text, body, trace_id, span_id, scope, resource, attributes`
	spanColumns = `ts, end_ts, ingest_ts, project, service, environment, version,
  trace_id, span_id, parent_span_id, name, kind, status_code, status_message,
  duration_ns, scope, resource, attributes, events, links`
	pointColumns = `ts, start_ts, ingest_ts, project, service, environment, version,
  name, unit, type, temporality, monotonic, series_hash, resource, attributes,
  value_int, value_double, count, sum, min, max, bucket_counts, bucket_bounds`
)

var columns = map[string]string{
	"logs":          logColumns,
	"spans":         spanColumns,
	"metric_points": pointColumns,
}

// Querier is satisfied by *sql.DB and *sql.Conn, so a load goroutine can run
// queries on its own connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Reader runs queries over hot and cold data.
type Reader struct {
	DB       Querier
	Manifest *manifest.Manifest
	DataDir  string
}

// Source returns a subquery over a signal's rows in [from, to): hot rows
// UNION ALL the given Parquet files, and the arguments for its placeholders.
// With no files it is hot only. Files are server-generated paths and are
// interpolated; the time bounds are parameters.
func Source(signal string, from, to time.Time, files []string) (string, []any) {
	cols := columns[signal]
	q := fmt.Sprintf("SELECT %s FROM %s WHERE ts >= ? AND ts < ?", cols, signal)
	args := []any{from, to}
	if len(files) > 0 {
		q += fmt.Sprintf("\nUNION ALL\nSELECT %s FROM %s WHERE ts >= ? AND ts < ?", cols, parquetSource(files))
		args = append(args, from, to)
	}
	return "(" + q + ")", args
}

func parquetSource(files []string) string {
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = "'" + strings.ReplaceAll(f, "'", "''") + "'"
	}
	return "read_parquet([" + strings.Join(quoted, ", ") + "], union_by_name = true)"
}

// source resolves the cold files for [from, to) from the manifest and builds
// the subquery.
func (r Reader) source(ctx context.Context, signal string, from, to time.Time) (string, []any, error) {
	if err := store.CheckSignal(signal); err != nil {
		return "", nil, err
	}
	files, err := r.Manifest.FilesFor(ctx, signal, from, to)
	if err != nil {
		return "", nil, err
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(r.DataDir, f.Path)
	}
	q, args := Source(signal, from, to, paths)
	return q, args, nil
}

// SeverityCount is one row of CountBySeverity.
type SeverityCount struct {
	Service  string
	Severity string
	Count    int64
}

const countBySeveritySQL = `SELECT service, coalesce(severity_text, ''), count(*)
FROM %s
GROUP BY ALL
ORDER BY 1, 2`

// CountBySeverity counts logs in [from, to) by service and severity.
func (r Reader) CountBySeverity(ctx context.Context, from, to time.Time) ([]SeverityCount, error) {
	src, args, err := r.source(ctx, "logs", from, to)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, fmt.Sprintf(countBySeveritySQL, src), args...)
	if err != nil {
		return nil, fmt.Errorf("count by severity: %w", err)
	}
	return collect(rows, func(rows *sql.Rows) (SeverityCount, error) {
		var c SeverityCount
		err := rows.Scan(&c.Service, &c.Severity, &c.Count)
		return c, err
	})
}

// Span is one row of TraceByID.
type Span struct {
	TS           time.Time
	Service      string
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	DurationNS   int64
}

const traceByIDSQL = `SELECT ts, service, trace_id, span_id, coalesce(parent_span_id, ''), name, duration_ns
FROM %s
WHERE trace_id = ?
ORDER BY ts, span_id`

// TraceByID returns every span of one trace in [from, to), scanning the
// whole window.
func (r Reader) TraceByID(ctx context.Context, traceID string, from, to time.Time) ([]Span, error) {
	src, args, err := r.source(ctx, "spans", from, to)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, fmt.Sprintf(traceByIDSQL, src), append(args, traceID)...)
	if err != nil {
		return nil, fmt.Errorf("trace by id: %w", err)
	}
	return collect(rows, func(rows *sql.Rows) (Span, error) {
		var s Span
		err := rows.Scan(&s.TS, &s.Service, &s.TraceID, &s.SpanID, &s.ParentSpanID, &s.Name, &s.DurationNS)
		s.TS = s.TS.UTC()
		return s, err
	})
}

// Log is one row of ErrorLogsPage.
type Log struct {
	TS         time.Time
	Service    string
	Severity   string
	Body       string
	TraceID    string
	Attributes string
}

// ErrorLogsPageSize is the page size of ErrorLogsPage.
const ErrorLogsPageSize = 100

// Attribute keys are flat OTel names containing dots, so the JSON path
// quotes them.
const errorLogsPageSQL = `SELECT ts, service, severity_text, coalesce(body, ''), coalesce(trace_id, ''), coalesce(attributes, '')
FROM %s
WHERE service = ? AND severity_text = 'ERROR'
  AND json_extract_string(attributes, '$."http.route"') = ?
ORDER BY ts DESC
LIMIT %d`

// ErrorLogsPage returns the newest error logs of one service for one route
// in [from, to).
func (r Reader) ErrorLogsPage(ctx context.Context, service, route string, from, to time.Time) ([]Log, error) {
	src, args, err := r.source(ctx, "logs", from, to)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(errorLogsPageSQL, src, ErrorLogsPageSize)
	rows, err := r.DB.QueryContext(ctx, q, append(args, service, route)...)
	if err != nil {
		return nil, fmt.Errorf("error logs page: %w", err)
	}
	return collect(rows, func(rows *sql.Rows) (Log, error) {
		var l Log
		err := rows.Scan(&l.TS, &l.Service, &l.Severity, &l.Body, &l.TraceID, &l.Attributes)
		l.TS = l.TS.UTC()
		return l, err
	})
}

// collect scans every row with scan and closes rows.
func collect[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error) {
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
