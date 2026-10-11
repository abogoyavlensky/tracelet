package query

import (
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

// Default ranges: what a search covers when it names none, and what a trace
// lookup covers.
const (
	DefaultLast      = time.Hour
	DefaultTraceLast = 7 * 24 * time.Hour
)

// ResolveRange turns the request's time parameters into an absolute range.
// last, when positive, is the range ending at now and excludes from and to;
// a lone from ends at now; a lone to starts defaultLast before it; nothing at
// all is the last defaultLast.
func ResolveRange(from, to time.Time, last, defaultLast time.Duration, now time.Time) (Range, error) {
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
		from = to.Add(-defaultLast)
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
