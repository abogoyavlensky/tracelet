package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LogsParams is one logs search. Exactly one of Range and Cursor positions
// it: a cursor carries its first page's range so later pages read the same
// window.
type LogsParams struct {
	ProjectID   string
	Service     string
	Environment string
	MinSeverity int16 // 0 is no floor
	Query       string
	TraceID     string
	Attrs       []Attr
	Range       Range
	Limit       int // 0 is DefaultLimit
	Cursor      string
}

// Log is one search result. Resource and Attributes are JSON objects, or nil.
type Log struct {
	TS             time.Time       `json:"ts"`
	ObservedTS     time.Time       `json:"observed_ts,omitzero"`
	Service        string          `json:"service"`
	Environment    string          `json:"environment,omitempty"`
	Version        string          `json:"version,omitempty"`
	SeverityNumber int16           `json:"severity_number"`
	SeverityText   string          `json:"severity_text,omitempty"`
	Body           string          `json:"body"`
	TraceID        string          `json:"trace_id,omitempty"`
	SpanID         string          `json:"span_id,omitempty"`
	Scope          string          `json:"scope,omitempty"`
	Resource       json.RawMessage `json:"resource,omitempty"`
	Attributes     json.RawMessage `json:"attributes,omitempty"`
}

// LogsPage is one page of results, newest first.
type LogsPage struct {
	Items      []Log  `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Range      Range  `json:"range"`
	Limit      int    `json:"limit"`
	Truncated  bool   `json:"truncated"`
}

const logsSelect = `SELECT ts, observed_ts, ingest_ts, service, environment, version,
  severity_number, severity_text, body, trace_id, span_id, scope, resource, attributes
FROM %s
WHERE %s
ORDER BY ts DESC, ingest_ts DESC
LIMIT ?`

// LogsSearch returns one page of a project's logs matching p, newest first
// by (ts, ingest_ts), which is unique per row.
func LogsSearch(ctx context.Context, snap *Snapshot, p LogsParams) (LogsPage, error) {
	limit := p.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return LogsPage{}, invalid("limit must be between 1 and %d", MaxLimit)
	}
	if p.ProjectID == "" {
		return LogsPage{}, invalid("project is required")
	}

	rng := p.Range
	var after *position
	if p.Cursor != "" {
		pos, err := decodeCursor(p.Cursor)
		if err != nil {
			return LogsPage{}, err
		}
		rng, after = Range{From: pos.From, To: pos.To}, &pos
	}
	if !rng.From.Before(rng.To) {
		return LogsPage{}, invalid("from must be before to")
	}

	var f filter
	f.add("project = ?", p.ProjectID)
	if p.Service != "" {
		f.add("service = ?", p.Service)
	}
	if p.Environment != "" {
		f.add("environment = ?", p.Environment)
	}
	if p.MinSeverity > 0 {
		f.add("severity_number >= ?", p.MinSeverity)
	}
	if p.Query != "" {
		f.add("contains(lower(body), lower(?))", p.Query)
	}
	if p.TraceID != "" {
		f.add("trace_id = ?", strings.ToLower(p.TraceID))
	}
	for _, a := range p.Attrs {
		f.add("json_extract_string(attributes, ?) = ?", attrPath(a.Key), a.Value)
	}
	if after != nil {
		f.add("(ts < ? OR (ts = ? AND ingest_ts < ?))", after.TS, after.TS, after.IngestTS)
	}

	src, srcArgs, err := snap.Source(ctx, "logs", rng.From, rng.To)
	if err != nil {
		return LogsPage{}, err
	}
	q := fmt.Sprintf(logsSelect, src, f.clause())
	all := append(append(srcArgs, f.args...), limit+1)
	rows, err := snap.QueryContext(ctx, q, all...)
	if err != nil {
		return LogsPage{}, fmt.Errorf("search logs: %w", err)
	}
	defer rows.Close()

	page := LogsPage{Items: []Log{}, Range: rng, Limit: limit}
	var last position
	for rows.Next() {
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(position{TS: last.TS, IngestTS: last.IngestTS, From: rng.From, To: rng.To})
			page.Truncated = true
			break
		}
		l, ingestTS, err := scanLog(rows)
		if err != nil {
			return LogsPage{}, err
		}
		page.Items = append(page.Items, l)
		last = position{TS: l.TS, IngestTS: ingestTS}
	}
	if err := rows.Err(); err != nil {
		return LogsPage{}, fmt.Errorf("search logs: %w", err)
	}
	return page, nil
}

func scanLog(rows *sql.Rows) (Log, time.Time, error) {
	var l Log
	var ingestTS time.Time
	var observed sql.NullTime
	var env, version, sevText, body, traceID, spanID, scope, resource, attrs sql.NullString
	var sev sql.NullInt16
	err := rows.Scan(&l.TS, &observed, &ingestTS, &l.Service, &env, &version,
		&sev, &sevText, &body, &traceID, &spanID, &scope, &resource, &attrs)
	if err != nil {
		return Log{}, time.Time{}, fmt.Errorf("scan log: %w", err)
	}
	l.TS, ingestTS = l.TS.UTC(), ingestTS.UTC()
	if observed.Valid {
		l.ObservedTS = observed.Time.UTC()
	}
	l.Environment, l.Version, l.SeverityText = env.String, version.String, sevText.String
	l.SeverityNumber = sev.Int16
	l.Body, l.TraceID, l.SpanID, l.Scope = body.String, traceID.String, spanID.String, scope.String
	if resource.Valid {
		l.Resource = json.RawMessage(resource.String)
	}
	if attrs.Valid {
		l.Attributes = json.RawMessage(attrs.String)
	}
	return l, ingestTS, nil
}

// ServiceCount is one service's log and span counts in a range.
type ServiceCount struct {
	Service string `json:"service"`
	Logs    int64  `json:"logs"`
	Spans   int64  `json:"spans"`
}

const servicesSQL = `SELECT service, sum(logs) AS logs, sum(spans) AS spans FROM (
  SELECT service, count(*) AS logs, 0 AS spans FROM %s WHERE project = ? GROUP BY service
  UNION ALL
  SELECT service, 0, count(*) FROM %s WHERE project = ? GROUP BY service
) GROUP BY service ORDER BY sum(logs) + sum(spans) DESC, service`

// Services returns the distinct services with logs or spans in a project and
// range, busiest first by the two counts together.
func Services(ctx context.Context, snap *Snapshot, projectID string, rng Range) ([]ServiceCount, error) {
	if !rng.From.Before(rng.To) {
		return nil, invalid("from must be before to")
	}
	logs, logArgs, err := snap.Source(ctx, "logs", rng.From, rng.To)
	if err != nil {
		return nil, err
	}
	spans, spanArgs, err := snap.Source(ctx, "spans", rng.From, rng.To)
	if err != nil {
		return nil, err
	}
	args := append(append(logArgs, projectID), spanArgs...)
	args = append(args, projectID)
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(servicesSQL, logs, spans), args...)
	if err != nil {
		return nil, fmt.Errorf("services: %w", err)
	}
	defer rows.Close()

	out := []ServiceCount{}
	for rows.Next() {
		var c ServiceCount
		if err := rows.Scan(&c.Service, &c.Logs, &c.Spans); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
