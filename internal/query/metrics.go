package query

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// Metric types as the API names them.
const (
	KindGauge     = "gauge"
	KindSum       = "sum"
	KindHistogram = "histogram"
)

// MetricKind is what a metric is: its type, whether a sum is monotonic, and
// its unit. A name normally has one kind; queries refuse to mix two.
type MetricKind struct {
	Type      string `json:"type"` // gauge | sum | histogram
	Monotonic bool   `json:"monotonic"`
	Unit      string `json:"unit,omitempty"`
}

func (k MetricKind) String() string {
	s := k.Type
	if k.Type == KindSum {
		if k.Monotonic {
			s = "monotonic sum"
		} else {
			s = "non-monotonic sum"
		}
	}
	if k.Unit != "" {
		s += " in " + k.Unit
	}
	return s
}

func typeName(code int16) string {
	switch code {
	case telemetry.TypeGauge:
		return KindGauge
	case telemetry.TypeSum:
		return KindSum
	default:
		return KindHistogram
	}
}

// isCounter reports whether the kind is a monotonic sum.
func (k MetricKind) isCounter() bool { return k.Type == KindSum && k.Monotonic }

// MetricInfo is one entry of the metric catalog.
type MetricInfo struct {
	Name string `json:"name"`
	MetricKind
	Services []string  `json:"services"`
	Series   int64     `json:"series"`
	Points   int64     `json:"points"`
	LastTS   time.Time `json:"last_ts"`
}

// MetricsParams filters the catalog. Query matches a case-insensitive
// substring of the name.
type MetricsParams struct {
	ProjectID, Service, Environment, Query string
	Range                                  Range
}

// MetricsList is the catalog of metrics in a range.
type MetricsList struct {
	Items     []MetricInfo `json:"items"`
	Range     Range        `json:"range"`
	Truncated bool         `json:"truncated"`
}

// MaxMetrics caps the catalog.
const MaxMetrics = 1000

const metricsListSQL = `SELECT name, type, monotonic, coalesce(unit, '') AS unit,
  list_sort(list_distinct(list(service))) AS services,
  count(DISTINCT series_hash), count(*), max(ts)
FROM %s
WHERE %s
GROUP BY name, type, monotonic, coalesce(unit, '')
ORDER BY name, type, monotonic, unit
LIMIT ?`

// ListMetrics returns the metrics with points in a range: one item per name
// and kind, ordered by name.
func ListMetrics(ctx context.Context, snap *Snapshot, p MetricsParams) (MetricsList, error) {
	if !p.Range.From.Before(p.Range.To) {
		return MetricsList{}, invalid("from must be before to")
	}
	src, args, err := snap.Source(ctx, "metric_points", p.Range.From, p.Range.To)
	if err != nil {
		return MetricsList{}, err
	}
	f := pointFilter(p.ProjectID, p.Service, p.Environment)
	if p.Query != "" {
		f.add("contains(lower(name), lower(?))", p.Query)
	}
	args = append(append(args, f.args...), MaxMetrics+1)
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(metricsListSQL, src, f.clause()), args...)
	if err != nil {
		return MetricsList{}, fmt.Errorf("list metrics: %w", err)
	}
	defer rows.Close()

	out := MetricsList{Items: []MetricInfo{}, Range: p.Range}
	for rows.Next() {
		var m MetricInfo
		var typ int16
		var services []any
		if err := rows.Scan(&m.Name, &typ, &m.Monotonic, &m.Unit, &services, &m.Series, &m.Points, &m.LastTS); err != nil {
			return MetricsList{}, fmt.Errorf("scan metric: %w", err)
		}
		m.Type, m.LastTS, m.Services = typeName(typ), m.LastTS.UTC(), strs(services)
		out.Items = append(out.Items, m)
	}
	if err := rows.Err(); err != nil {
		return MetricsList{}, fmt.Errorf("list metrics: %w", err)
	}
	if len(out.Items) > MaxMetrics {
		out.Items, out.Truncated = out.Items[:MaxMetrics], true
	}
	return out, nil
}

// pointFilter starts a metric_points filter on project, service, and
// environment; empty values do not filter.
func pointFilter(projectID, service, environment string) *filter {
	f := &filter{}
	f.add("project = ?", projectID)
	if service != "" {
		f.add("service = ?", service)
	}
	if environment != "" {
		f.add("environment = ?", environment)
	}
	return f
}

// AttributeInfo is one point attribute key of a metric: how many distinct
// values it has and the first of them in order.
type AttributeInfo struct {
	Key      string   `json:"key"`
	Distinct int64    `json:"distinct"`
	Values   []string `json:"values"`
}

// MetricAttributesParams names the metric whose attributes to list.
type MetricAttributesParams struct {
	ProjectID, Name, Service, Environment string
	Range                                 Range
}

// MetricAttributes is a metric's point attribute keys.
type MetricAttributes struct {
	Name  string          `json:"name"`
	Items []AttributeInfo `json:"items"`
	Range Range           `json:"range"`
}

// Attribute discovery caps.
const (
	MaxAttributeKeys   = 100
	MaxAttributeValues = 20
)

// The value path quotes the key as attrPath does, so a dotted key matches
// itself rather than a nested path.
const metricAttributesSQL = `WITH kv AS (
  SELECT key, json_extract_string(attributes,
           '$."' || replace(replace(key, '\', '\\'), '"', '\"') || '"') AS v
  FROM (SELECT unnest(json_keys(attributes)) AS key, attributes FROM %s WHERE %s AND attributes IS NOT NULL)
)
SELECT key, count(DISTINCT v), list_sort(list_distinct(list(v)))[1:?]
FROM kv
GROUP BY key
ORDER BY key
LIMIT ?`

// ListMetricAttributes returns the point attribute keys of one metric in a
// range, ordered by key.
func ListMetricAttributes(ctx context.Context, snap *Snapshot, p MetricAttributesParams) (MetricAttributes, error) {
	if p.Name == "" {
		return MetricAttributes{}, invalid("name is required")
	}
	if !p.Range.From.Before(p.Range.To) {
		return MetricAttributes{}, invalid("from must be before to")
	}
	src, args, err := snap.Source(ctx, "metric_points", p.Range.From, p.Range.To)
	if err != nil {
		return MetricAttributes{}, err
	}
	f := pointFilter(p.ProjectID, p.Service, p.Environment)
	f.add("name = ?", p.Name)
	args = append(append(args, f.args...), MaxAttributeValues, MaxAttributeKeys)
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(metricAttributesSQL, src, f.clause()), args...)
	if err != nil {
		return MetricAttributes{}, fmt.Errorf("metric attributes: %w", err)
	}
	defer rows.Close()

	out := MetricAttributes{Name: p.Name, Items: []AttributeInfo{}, Range: p.Range}
	for rows.Next() {
		var a AttributeInfo
		var values []any
		if err := rows.Scan(&a.Key, &a.Distinct, &values); err != nil {
			return MetricAttributes{}, fmt.Errorf("scan attribute: %w", err)
		}
		a.Values = strs(values)
		out.Items = append(out.Items, a)
	}
	if err := rows.Err(); err != nil {
		return MetricAttributes{}, fmt.Errorf("metric attributes: %w", err)
	}
	return out, nil
}

// strs converts a scanned DuckDB list of strings, skipping NULLs.
func strs(xs []any) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Aggregate names.
const (
	AggRate     = "rate"
	AggIncrease = "increase"
	AggAvg      = "avg"
	AggMin      = "min"
	AggMax      = "max"
	AggSum      = "sum"
)

// Aggregate is a parsed aggregation. Quantile is set for pNN.
type Aggregate struct {
	Name     string
	Quantile float64
}

// Aggregations valid for each kind, the default first. Histograms also take
// any pNN or pNN.N.
var (
	counterAggregates   = []string{AggRate, AggIncrease}
	gaugeAggregates     = []string{AggAvg, AggMin, AggMax, AggSum}
	histogramAggregates = []string{"p95", AggRate, AggAvg}
)

// Aggregates returns the aggregations a kind accepts, the default first.
func Aggregates(kind MetricKind) []string {
	switch {
	case kind.isCounter():
		return counterAggregates
	case kind.Type == KindHistogram:
		return histogramAggregates
	default:
		return gaugeAggregates
	}
}

// ParseAggregate checks an aggregation against a kind; "" is the kind's
// default.
func ParseAggregate(kind MetricKind, s string) (Aggregate, error) {
	valid := Aggregates(kind)
	if s == "" {
		s = valid[0]
	}
	if kind.Type == KindHistogram {
		if q, ok := parseQuantile(s); ok {
			return Aggregate{Name: s, Quantile: q}, nil
		}
	}
	if slices.Contains(valid, s) {
		return Aggregate{Name: s}, nil
	}
	list := strings.Join(valid, ", ")
	if kind.Type == KindHistogram {
		list = "pNN (such as p50 or p99.9), rate, avg"
	}
	return Aggregate{}, invalid("aggregate %q does not apply to a %s; use one of %s", s, kind, list)
}

// quantilePattern is pNN or pNN.N.
var quantilePattern = regexp.MustCompile(`^p([0-9]{1,2}(?:\.[0-9])?)$`)

// parseQuantile reads pNN or pNN.N with a percentile strictly between 0 and
// 100, and returns it as a fraction.
func parseQuantile(s string) (float64, bool) {
	m := quantilePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil || pct <= 0 {
		return 0, false
	}
	return pct / 100, true
}

// Step bounds.
const (
	MinStep  = 10 * time.Second
	MaxSteps = 1500
)

// autoSteps are the steps an automatic choice picks from.
var autoSteps = []time.Duration{
	10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// ResolveStep returns the step for a range: step itself when positive, after
// checking it, or else the first automatic step of at least max(range/200,
// 1m).
func ResolveStep(rng Range, step time.Duration) (time.Duration, error) {
	span := rng.To.Sub(rng.From)
	if step < 0 {
		return 0, invalid("step must be positive")
	}
	if step > 0 {
		if step < MinStep {
			return 0, invalid("step must be at least %s", MinStep)
		}
		if step%time.Second != 0 {
			return 0, invalid("step must be a whole number of seconds")
		}
		if n := (span + step - 1) / step; n > MaxSteps {
			return 0, invalid("step %s gives %d steps over the range; at most %d are allowed", step, n, MaxSteps)
		}
		return step, nil
	}
	want := max(span/200, time.Minute)
	for _, s := range autoSteps {
		if s >= want {
			return s, nil
		}
	}
	return autoSteps[len(autoSteps)-1], nil
}
