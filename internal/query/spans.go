package query

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Span is one span as the API returns it. Resource and Attributes are JSON
// objects, Events and Links JSON arrays, or nil. Kind and StatusCode are the
// OTLP enum numbers.
type Span struct {
	TS            time.Time       `json:"ts"`
	EndTS         time.Time       `json:"end_ts"`
	DurationNS    int64           `json:"duration_ns"`
	Service       string          `json:"service"`
	Environment   string          `json:"environment,omitempty"`
	Version       string          `json:"version,omitempty"`
	TraceID       string          `json:"trace_id"`
	SpanID        string          `json:"span_id"`
	ParentSpanID  string          `json:"parent_span_id,omitempty"`
	Name          string          `json:"name"`
	Kind          int16           `json:"kind"`
	StatusCode    int16           `json:"status_code"`
	StatusMessage string          `json:"status_message,omitempty"`
	Scope         string          `json:"scope,omitempty"`
	Resource      json.RawMessage `json:"resource,omitempty"`
	Attributes    json.RawMessage `json:"attributes,omitempty"`
	Events        json.RawMessage `json:"events,omitempty"`
	Links         json.RawMessage `json:"links,omitempty"`
}

const spanSelect = `SELECT ts, end_ts, ingest_ts, service, environment, version,
  trace_id, span_id, parent_span_id, name, kind, status_code, status_message,
  duration_ns, scope, resource, attributes, events, links
FROM %s
WHERE %s
ORDER BY %s
LIMIT ?`

func scanSpan(rows *sql.Rows) (Span, time.Time, error) {
	var s Span
	var ingestTS time.Time
	var env, version, parent, statusMsg, scope, resource, attrs, events, links sql.NullString
	err := rows.Scan(&s.TS, &s.EndTS, &ingestTS, &s.Service, &env, &version,
		&s.TraceID, &s.SpanID, &parent, &s.Name, &s.Kind, &s.StatusCode, &statusMsg,
		&s.DurationNS, &scope, &resource, &attrs, &events, &links)
	if err != nil {
		return Span{}, time.Time{}, fmt.Errorf("scan span: %w", err)
	}
	s.TS, s.EndTS, ingestTS = s.TS.UTC(), s.EndTS.UTC(), ingestTS.UTC()
	s.Environment, s.Version, s.ParentSpanID = env.String, version.String, parent.String
	s.StatusMessage, s.Scope = statusMsg.String, scope.String
	s.Resource, s.Attributes = rawJSON(resource), rawJSON(attrs)
	s.Events, s.Links = rawJSON(events), rawJSON(links)
	return s, ingestTS, nil
}

func rawJSON(s sql.NullString) json.RawMessage {
	if !s.Valid {
		return nil
	}
	return json.RawMessage(s.String)
}

// ParseTraceID checks a trace ID is 32 hex characters and lower-cases it,
// which is how trace IDs are stored.
func ParseTraceID(s string) (string, error) {
	if len(s) != 32 {
		return "", invalid("trace ID must be 32 hex characters")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", invalid("trace ID must be 32 hex characters")
	}
	return strings.ToLower(s), nil
}

// Span status codes by name, as OTLP numbers them.
var statusCodes = map[string]int16{"unset": 0, "ok": 1, "error": 2}

// ParseStatus returns the status code a name stands for; ok is false when s
// is empty, meaning any status.
func ParseStatus(s string) (code int16, ok bool, err error) {
	if s == "" {
		return 0, false, nil
	}
	code, ok = statusCodes[strings.ToLower(s)]
	if !ok {
		return 0, false, invalid("status must be one of unset, ok, error")
	}
	return code, true, nil
}

// SpansParams is one span search. As for logs, exactly one of Range and
// Cursor positions it.
type SpansParams struct {
	ProjectID     string
	Service       string
	Environment   string
	Name          string
	Status        *int16 // nil is any status
	MinDurationNS int64
	Root          bool // only spans without a parent
	TraceID       string
	Attrs         []Attr
	Range         Range
	Limit         int // 0 is DefaultLimit
	Cursor        string
}

// SpansPage is one page of spans, newest first.
type SpansPage struct {
	Items      []Span `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Range      Range  `json:"range"`
	Limit      int    `json:"limit"`
	Truncated  bool   `json:"truncated"`
}

// SpansSearch returns one page of a project's spans matching p, newest first
// by (ts, ingest_ts). Root spans stand in for a list of traces; a trace whose
// root was never exported has none.
func SpansSearch(ctx context.Context, snap *Snapshot, p SpansParams) (SpansPage, error) {
	limit := p.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return SpansPage{}, invalid("limit must be between 1 and %d", MaxLimit)
	}
	if p.ProjectID == "" {
		return SpansPage{}, invalid("project is required")
	}
	if p.MinDurationNS < 0 {
		return SpansPage{}, invalid("min_duration must be positive")
	}

	rng := p.Range
	var after *position
	if p.Cursor != "" {
		pos, err := decodeCursor(p.Cursor)
		if err != nil {
			return SpansPage{}, err
		}
		rng, after = Range{From: pos.From, To: pos.To}, &pos
	}
	if !rng.From.Before(rng.To) {
		return SpansPage{}, invalid("from must be before to")
	}

	var f filter
	f.add("project = ?", p.ProjectID)
	if p.Service != "" {
		f.add("service = ?", p.Service)
	}
	if p.Environment != "" {
		f.add("environment = ?", p.Environment)
	}
	if p.Name != "" {
		f.add("name = ?", p.Name)
	}
	if p.Status != nil {
		f.add("status_code = ?", *p.Status)
	}
	if p.MinDurationNS > 0 {
		f.add("duration_ns >= ?", p.MinDurationNS)
	}
	if p.Root {
		f.add("parent_span_id IS NULL")
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

	src, srcArgs, err := snap.Source(ctx, "spans", rng.From, rng.To)
	if err != nil {
		return SpansPage{}, err
	}
	q := fmt.Sprintf(spanSelect, src, f.clause(), "ts DESC, ingest_ts DESC")
	rows, err := snap.QueryContext(ctx, q, append(append(srcArgs, f.args...), limit+1)...)
	if err != nil {
		return SpansPage{}, fmt.Errorf("search spans: %w", err)
	}
	defer rows.Close()

	page := SpansPage{Items: []Span{}, Range: rng, Limit: limit}
	var last position
	for rows.Next() {
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(position{TS: last.TS, IngestTS: last.IngestTS, From: rng.From, To: rng.To})
			page.Truncated = true
			break
		}
		s, ingestTS, err := scanSpan(rows)
		if err != nil {
			return SpansPage{}, err
		}
		page.Items = append(page.Items, s)
		last = position{TS: s.TS, IngestTS: ingestTS}
	}
	if err := rows.Err(); err != nil {
		return SpansPage{}, fmt.Errorf("search spans: %w", err)
	}
	return page, nil
}

// MaxTraceSpans caps the spans one trace lookup returns.
const MaxTraceSpans = 5000

// ErrTraceNotFound is a trace with no spans in the range.
var ErrTraceNotFound = errors.New("trace not found")

// TraceParams is one trace lookup.
type TraceParams struct {
	ProjectID string
	TraceID   string
	Range     Range
	Limit     int // 0 is MaxTraceSpans
}

// Trace is one trace's spans, oldest first, flat: clients nest them by
// ParentSpanID. Start, End, DurationNS, and Services describe the returned
// spans, which is the whole trace unless Truncated is true.
type Trace struct {
	TraceID    string    `json:"trace_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationNS int64     `json:"duration_ns"`
	Services   []string  `json:"services"`
	Spans      []Span    `json:"spans"`
	Range      Range     `json:"range"`
	Truncated  bool      `json:"truncated"`
}

// TraceGet returns a project's spans of one trace within the range, oldest
// first by (ts, ingest_ts). Span files are sorted by trace ID in small row
// groups, so each file contributes at most one row group to the lookup.
func TraceGet(ctx context.Context, snap *Snapshot, p TraceParams) (Trace, error) {
	traceID, err := ParseTraceID(p.TraceID)
	if err != nil {
		return Trace{}, err
	}
	if p.ProjectID == "" {
		return Trace{}, invalid("project is required")
	}
	if !p.Range.From.Before(p.Range.To) {
		return Trace{}, invalid("from must be before to")
	}
	limit := p.Limit
	if limit == 0 {
		limit = MaxTraceSpans
	}

	src, args, err := snap.Source(ctx, "spans", p.Range.From, p.Range.To)
	if err != nil {
		return Trace{}, err
	}
	q := fmt.Sprintf(spanSelect, src, "project = ? AND trace_id = ?", "ts, ingest_ts")
	rows, err := snap.QueryContext(ctx, q, append(args, p.ProjectID, traceID, limit+1)...)
	if err != nil {
		return Trace{}, fmt.Errorf("get trace: %w", err)
	}
	defer rows.Close()

	tr := Trace{TraceID: traceID, Spans: []Span{}, Range: p.Range}
	for rows.Next() {
		if len(tr.Spans) == limit {
			tr.Truncated = true
			break
		}
		s, _, err := scanSpan(rows)
		if err != nil {
			return Trace{}, err
		}
		tr.Spans = append(tr.Spans, s)
	}
	if err := rows.Err(); err != nil {
		return Trace{}, fmt.Errorf("get trace: %w", err)
	}
	if len(tr.Spans) == 0 {
		return Trace{}, ErrTraceNotFound
	}

	tr.Start, tr.End = tr.Spans[0].TS, tr.Spans[0].EndTS
	tr.Services = []string{}
	for _, s := range tr.Spans {
		if s.EndTS.After(tr.End) {
			tr.End = s.EndTS
		}
		if !slices.Contains(tr.Services, s.Service) {
			tr.Services = append(tr.Services, s.Service)
		}
	}
	slices.Sort(tr.Services)
	tr.DurationNS = max(tr.End.Sub(tr.Start).Nanoseconds(), 0)
	return tr, nil
}
