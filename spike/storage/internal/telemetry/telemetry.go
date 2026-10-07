// Package telemetry defines the row value types that map one to one onto the
// hot tables and the Parquet files.
package telemetry

import (
	"hash/fnv"
	"maps"
	"slices"
	"time"
)

// Metric point types and temporalities as stored in metric_points.
const (
	TypeGauge     = 1
	TypeSum       = 2
	TypeHistogram = 3

	TemporalityDelta      = 1
	TemporalityCumulative = 2
)

// Log is one row of the logs table. Empty strings are stored as NULL.
type Log struct {
	TS             time.Time
	ObservedTS     time.Time // zero is NULL
	IngestTS       time.Time
	Project        string
	Service        string
	Environment    string
	Version        string
	SeverityNumber int16
	SeverityText   string
	Body           string
	TraceID        string
	SpanID         string
	Scope          string
	Resource       string // JSON text
	Attributes     string // JSON text
}

// Span is one row of the spans table. Empty strings are stored as NULL.
type Span struct {
	TS            time.Time
	EndTS         time.Time
	IngestTS      time.Time
	Project       string
	Service       string
	Environment   string
	Version       string
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Name          string
	Kind          int16
	StatusCode    int16
	StatusMessage string
	DurationNS    int64
	Scope         string
	Resource      string // JSON text
	Attributes    string // JSON text
	Events        string // JSON text
	Links         string // JSON text
}

// MetricPoint is one row of the metric_points table. Empty strings and nil
// pointers are stored as NULL; pointers are used where zero is a real value.
type MetricPoint struct {
	TS           time.Time
	StartTS      time.Time // zero is NULL
	IngestTS     time.Time
	Project      string
	Service      string
	Environment  string
	Version      string
	Name         string
	Unit         string
	Type         int16
	Temporality  int16
	Monotonic    bool
	SeriesHash   uint64
	Resource     string // JSON text
	Attributes   string // JSON text
	ValueInt     *int64
	ValueDouble  *float64
	Count        *uint64
	Sum          *float64
	Min          *float64
	Max          *float64
	BucketCounts []uint64 // nil is NULL
	BucketBounds []float64
}

// Batch is one write's worth of rows across all signals.
type Batch struct {
	Logs   []Log
	Spans  []Span
	Points []MetricPoint
}

// Len returns the total row count.
func (b Batch) Len() int { return len(b.Logs) + len(b.Spans) + len(b.Points) }

// SeriesHash identifies a metric series: FNV-64a over the name, the service,
// and the attribute pairs sorted by key, all joined with NUL.
func SeriesHash(name, service string, attrs map[string]string) uint64 {
	h := fnv.New64a()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(name)
	write(service)
	for _, k := range slices.Sorted(maps.Keys(attrs)) {
		write(k)
		write(attrs[k])
	}
	return h.Sum64()
}
