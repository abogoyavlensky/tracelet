package query

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
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

// Source returns a subquery over a signal's rows in [from, to) and the
// arguments for its placeholders: hot rows UNION ALL the Parquet files the
// manifest lists for the range.
//
// The manifest is read once. For every hour that has rows in hot and files in
// that list, hot rows stamped at or below the hour's greatest file cutoff are
// excluded, because the files hold them. Together with the snapshot, which
// predates any later hot DELETE, a flush can neither duplicate nor hide an
// hour. File paths are server-generated and interpolated; everything else is
// a parameter.
//
// Each call reads the manifest afresh, so rows committed after Open and
// flushed before a later call can appear in that call's cold half. Nothing is
// counted twice or missed, but two Source calls on one Snapshot are not a
// repeatable read; queries that must agree with each other share one call.
func (s *Snapshot) Source(ctx context.Context, signal string, from, to time.Time) (string, []any, error) {
	if err := duckdb.CheckSignal(signal); err != nil {
		return "", nil, err
	}
	hotHours, err := duckdb.HotHours(ctx, s.tx, signal)
	if err != nil {
		return "", nil, err
	}
	files, err := s.manifest.FilesFor(ctx, signal, from, to)
	if err != nil {
		return "", nil, err
	}

	cutoffs := map[time.Time]time.Time{}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(s.dataDir, f.Path)
		if c, ok := cutoffs[f.Hour]; !ok || f.MaxIngestTS.After(c) {
			cutoffs[f.Hour] = f.MaxIngestTS
		}
	}

	cols := columns[signal]
	q := fmt.Sprintf("SELECT %s FROM %s WHERE ts >= ? AND ts < ?", cols, signal)
	args := []any{from, to}
	for _, hour := range hotHours {
		cutoff, ok := cutoffs[hour]
		if !ok {
			continue
		}
		q += " AND NOT (ts >= ? AND ts < ? AND ingest_ts <= ?)"
		args = append(args, hour, hour.Add(time.Hour), cutoff)
	}
	if len(paths) > 0 {
		slices.Sort(paths)
		q += fmt.Sprintf("\nUNION ALL\nSELECT %s FROM %s WHERE ts >= ? AND ts < ?", cols, parquetSource(paths))
		args = append(args, from, to)
	}
	return "(" + q + ")", args, nil
}

func parquetSource(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", "''") + "'"
	}
	return "read_parquet([" + strings.Join(quoted, ", ") + "], union_by_name = true)"
}
