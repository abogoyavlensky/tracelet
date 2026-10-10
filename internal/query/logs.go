package query

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ValidationError reports a bad query parameter; its message is safe to show
// to the client.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Range is the half-open time range [From, To).
type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// DefaultLast is the range a search covers when it names none.
const DefaultLast = time.Hour

// ResolveRange turns the request's time parameters into an absolute range.
// last, when positive, is the range ending at now and excludes from and to;
// a lone from ends at now; a lone to starts DefaultLast before it; nothing at
// all is the last DefaultLast.
func ResolveRange(from, to time.Time, last time.Duration, now time.Time) (Range, error) {
	now = now.UTC()
	switch {
	case last < 0:
		return Range{}, invalid("last must be positive")
	case last > 0 && (!from.IsZero() || !to.IsZero()):
		return Range{}, invalid("last cannot be combined with from or to")
	case last > 0:
		return Range{From: now.Add(-last), To: now}, nil
	}
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-DefaultLast)
	}
	if !from.Before(to) {
		return Range{}, invalid("from must be before to")
	}
	return Range{From: from.UTC(), To: to.UTC()}, nil
}

// Severity floors of the OTLP severity number ranges, by level name.
var severityFloors = map[string]int16{
	"trace": 1, "debug": 5, "info": 9, "warn": 13, "error": 17, "fatal": 21,
}

// ParseLevel returns the lowest severity number of a level name: "error" is
// 17, so a minimum level of error matches 17 and above.
func ParseLevel(name string) (int16, error) {
	n, ok := severityFloors[strings.ToLower(name)]
	if !ok {
		return 0, invalid("level must be one of trace, debug, info, warn, error, fatal")
	}
	return n, nil
}

// Attr is an exact-match filter on a top-level attribute.
type Attr struct {
	Key, Value string
}

// ParseAttr reads "key:value". The key is everything before the first colon,
// so values may contain colons.
func ParseAttr(s string) (Attr, error) {
	k, v, ok := strings.Cut(s, ":")
	if !ok || k == "" {
		return Attr{}, invalid("attr must be key:value")
	}
	return Attr{Key: k, Value: v}, nil
}

// Search limits.
const (
	DefaultLimit = 100
	MaxLimit     = 1000
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

// position is a cursor: the last item's row key and the page's range.
type position struct {
	TS       time.Time `json:"ts"`
	IngestTS time.Time `json:"ingest_ts"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

func encodeCursor(p position) string {
	b, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("encode cursor: %v", err)) // times always marshal
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (position, error) {
	var p position
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return p, invalid("invalid cursor")
	}
	if err := json.Unmarshal(b, &p); err != nil || p.TS.IsZero() || !p.From.Before(p.To) {
		return p, invalid("invalid cursor")
	}
	return p, nil
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

	where := []string{"project = ?"}
	args := []any{p.ProjectID}
	add := func(cond string, a ...any) {
		where = append(where, cond)
		args = append(args, a...)
	}
	if p.Service != "" {
		add("service = ?", p.Service)
	}
	if p.Environment != "" {
		add("environment = ?", p.Environment)
	}
	if p.MinSeverity > 0 {
		add("severity_number >= ?", p.MinSeverity)
	}
	if p.Query != "" {
		add("contains(lower(body), lower(?))", p.Query)
	}
	if p.TraceID != "" {
		add("trace_id = ?", strings.ToLower(p.TraceID))
	}
	for _, a := range p.Attrs {
		add("json_extract_string(attributes, ?) = ?", attrPath(a.Key), a.Value)
	}
	if after != nil {
		add("(ts < ? OR (ts = ? AND ingest_ts < ?))", after.TS, after.TS, after.IngestTS)
	}

	src, srcArgs, err := snap.Source(ctx, "logs", rng.From, rng.To)
	if err != nil {
		return LogsPage{}, err
	}
	q := fmt.Sprintf(logsSelect, src, strings.Join(where, " AND "))
	all := append(append(srcArgs, args...), limit+1)
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

// attrPath is the JSON path of a top-level key, quoted so a dotted OTLP name
// such as exception.type matches the key rather than a nested path.
func attrPath(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	key = strings.ReplaceAll(key, `"`, `\"`)
	return `$."` + key + `"`
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

// ServiceCount is one service's log count in a range.
type ServiceCount struct {
	Service string `json:"service"`
	Logs    int64  `json:"logs"`
}

const servicesSQL = `SELECT service, count(*) AS n FROM %s WHERE project = ? GROUP BY service ORDER BY n DESC, service`

// Services returns the distinct services with logs in a project and range,
// busiest first.
func Services(ctx context.Context, snap *Snapshot, projectID string, rng Range) ([]ServiceCount, error) {
	if !rng.From.Before(rng.To) {
		return nil, invalid("from must be before to")
	}
	src, args, err := snap.Source(ctx, "logs", rng.From, rng.To)
	if err != nil {
		return nil, err
	}
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(servicesSQL, src), append(args, projectID)...)
	if err != nil {
		return nil, fmt.Errorf("services: %w", err)
	}
	defer rows.Close()

	out := []ServiceCount{}
	for rows.Next() {
		var c ServiceCount
		if err := rows.Scan(&c.Service, &c.Logs); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
