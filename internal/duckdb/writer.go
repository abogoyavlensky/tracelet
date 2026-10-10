package duckdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	duckdbgo "github.com/duckdb/duckdb-go/v2"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// CommitStats describes one Write.
type CommitStats struct {
	Rows    int
	Latency time.Duration // first append to commit
}

// Writer appends batches to the hot tables over one dedicated connection. It
// is not safe for concurrent use: the single writer is what makes ingest
// stamps commit in increasing order. Every row gets its own stamp, one
// microsecond after the previous row's, so (ts, ingest_ts) identifies a row.
type Writer struct {
	conn   *sql.Conn
	logs   *duckdbgo.Appender
	spans  *duckdbgo.Appender
	points *duckdbgo.Appender
	last   time.Time // ingest stamp of the last row written
}

const maxIngestSQL = `SELECT max(m) FROM (
  SELECT max(ingest_ts) AS m FROM logs
  UNION ALL SELECT max(ingest_ts) FROM spans
  UNION ALL SELECT max(ingest_ts) FROM metric_points)`

// NewWriter opens a dedicated connection and one appender per hot table, and
// seeds the last stamp from the hot tables so stamps keep increasing across
// restarts and a clock that stepped backwards.
func NewWriter(ctx context.Context, s *Store) (*Writer, error) {
	conn, err := s.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("writer connection: %w", err)
	}

	w := &Writer{conn: conn}
	var last sql.NullTime
	if err := conn.QueryRowContext(ctx, maxIngestSQL).Scan(&last); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("seed ingest stamp: %w", err)
	}
	if last.Valid {
		w.last = last.Time.UTC()
	}
	err = conn.Raw(func(dc any) error {
		raw, ok := dc.(driver.Conn)
		if !ok {
			return errors.New("not a driver connection")
		}
		var err error
		if w.logs, err = duckdbgo.NewAppenderFromConn(raw, "", "logs"); err != nil {
			return fmt.Errorf("logs appender: %w", err)
		}
		if w.spans, err = duckdbgo.NewAppenderFromConn(raw, "", "spans"); err != nil {
			return fmt.Errorf("spans appender: %w", err)
		}
		if w.points, err = duckdbgo.NewAppenderFromConn(raw, "", "metric_points"); err != nil {
			return fmt.Errorf("metric_points appender: %w", err)
		}
		return nil
	})
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	return w, nil
}

// Write stamps the rows with strictly increasing ingest times, the first at
// max(now, last + 1µs) and each next one a microsecond later, and commits the
// batch in one transaction. The stamps on the caller's batch are left
// untouched; rows are copied as they are appended.
func (w *Writer) Write(ctx context.Context, batch telemetry.Batch) (CommitStats, error) {
	if batch.Len() == 0 {
		return CommitStats{}, nil
	}

	stamp := time.Now().UTC().Truncate(time.Microsecond)
	if !stamp.After(w.last) {
		stamp = w.last.Add(time.Microsecond)
	}

	start := time.Now()
	if _, err := w.conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return CommitStats{}, fmt.Errorf("begin: %w", err)
	}
	next, err := w.appendAll(batch, stamp)
	if err != nil {
		// ROLLBACK does not touch the appenders' own buffers; without Clear the
		// rejected rows would be flushed by the next Write.
		w.clear()
		_, _ = w.conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		return CommitStats{}, err
	}
	if _, err := w.conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = w.conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		return CommitStats{}, fmt.Errorf("commit: %w", err)
	}
	w.last = next.Add(-time.Microsecond)

	return CommitStats{Rows: batch.Len(), Latency: time.Since(start)}, nil
}

// appendAll appends every row, stamping from stamp upwards, and returns the
// stamp the next row would get.
func (w *Writer) appendAll(batch telemetry.Batch, stamp time.Time) (time.Time, error) {
	next := func() time.Time {
		s := stamp
		stamp = stamp.Add(time.Microsecond)
		return s
	}
	for _, l := range batch.Logs {
		err := w.logs.AppendRow(
			l.TS, nullTime(l.ObservedTS), next(),
			l.Project, l.Service, nullString(l.Environment), nullString(l.Version),
			l.SeverityNumber, nullString(l.SeverityText), nullString(l.Body),
			nullString(l.TraceID), nullString(l.SpanID), nullString(l.Scope),
			nullString(l.Resource), nullString(l.Attributes),
		)
		if err != nil {
			return time.Time{}, fmt.Errorf("append log: %w", err)
		}
	}
	for _, s := range batch.Spans {
		err := w.spans.AppendRow(
			s.TS, s.EndTS, next(),
			s.Project, s.Service, nullString(s.Environment), nullString(s.Version),
			s.TraceID, s.SpanID, nullString(s.ParentSpanID),
			s.Name, s.Kind, s.StatusCode, nullString(s.StatusMessage),
			s.DurationNS, nullString(s.Scope),
			nullString(s.Resource), nullString(s.Attributes), nullString(s.Events), nullString(s.Links),
		)
		if err != nil {
			return time.Time{}, fmt.Errorf("append span: %w", err)
		}
	}
	for _, p := range batch.Points {
		err := w.points.AppendRow(
			p.TS, nullTime(p.StartTS), next(),
			p.Project, p.Service, nullString(p.Environment), nullString(p.Version),
			p.Name, nullString(p.Unit), p.Type, p.Temporality,
			p.Monotonic, p.SeriesHash,
			nullString(p.Resource), nullString(p.Attributes),
			nullPtr(p.ValueInt), nullPtr(p.ValueDouble),
			nullPtr(p.Count), nullPtr(p.Sum), nullPtr(p.Min), nullPtr(p.Max),
			nullSlice(p.BucketCounts), nullSlice(p.BucketBounds),
		)
		if err != nil {
			return time.Time{}, fmt.Errorf("append metric point: %w", err)
		}
	}

	for _, a := range []*duckdbgo.Appender{w.logs, w.spans, w.points} {
		if err := a.Flush(); err != nil {
			return time.Time{}, fmt.Errorf("flush appender: %w", err)
		}
	}
	return stamp, nil
}

func (w *Writer) clear() {
	for _, a := range []*duckdbgo.Appender{w.logs, w.spans, w.points} {
		_ = a.Clear()
	}
}

// Close closes the appenders and the connection.
func (w *Writer) Close() error {
	var errs []error
	for _, a := range []*duckdbgo.Appender{w.logs, w.spans, w.points} {
		if a != nil {
			errs = append(errs, a.Close())
		}
	}
	errs = append(errs, w.conn.Close())
	return errors.Join(errs...)
}

// The appender writes NULL only for an untyped nil (or a nil *time.Time); it
// does not unwrap sql.Null* values or other pointers, and a nil slice becomes
// an empty list. These helpers turn the value types' "absent" encodings (empty
// string, zero time, nil pointer, nil slice) into that untyped nil.

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullPtr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullSlice[T any](s []T) any {
	if s == nil {
		return nil
	}
	return s
}
