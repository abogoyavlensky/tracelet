package query

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// metricLookback is how far before the range a metric query reads, so the
// first point in the range finds its predecessor at default 60 s export
// intervals.
const metricLookback = 10 * time.Minute

// MaxMetricSeries caps the series one query returns.
const MaxMetricSeries = 20

// ErrMetricNotFound means the metric has no points in the range.
var ErrMetricNotFound = errors.New("metric not found")

// MetricQueryParams is one metric query.
type MetricQueryParams struct {
	ProjectID, Name, Service, Environment string
	Attrs                                 []Attr
	GroupBy                               string        // "", service, environment, version, or a point attribute key
	Aggregate                             string        // "" means the kind's default
	Step                                  time.Duration // 0 means auto
	Range                                 Range
}

// MetricSeries is one group's values, one per timestamp of the result, nil
// where the step has no data.
type MetricSeries struct {
	Group              map[string]string `json:"group"`
	Values             []*float64        `json:"values"`
	IncompatibleSeries *int              `json:"incompatible_series,omitempty"`
}

// MetricResult is a metric over aligned steps.
type MetricResult struct {
	Name string `json:"name"`
	MetricKind
	Aggregate   string         `json:"aggregate"`
	GroupBy     string         `json:"group_by,omitempty"`
	StepSeconds int64          `json:"step_seconds"`
	Range       Range          `json:"range"`
	Timestamps  []time.Time    `json:"timestamps"`
	Series      []MetricSeries `json:"series"`
	Truncated   bool           `json:"truncated"`
}

// deltasCTE turns the selected points into per-point deltas. Per series
// ordered by ts, a cumulative point is a reset when its value or count drops
// or its start_ts changes, and a reset's delta is its own value. A
// cumulative point with no predecessor whose series started inside the rows
// read (is_fresh) counts its whole value too. Otherwise the delta is the
// difference from the previous point, element-wise for buckets; a bounds
// change without a reset nulls only the bucket delta, because count and sum
// do not depend on the buckets. Delta-temporality points are used as they
// are. Only deltas at or after the range start are kept.
//
// Placeholders in textual order: the group expression's, the source's, the
// filter's, the start of the rows read (is_fresh), and the range start.
const deltasCTE = `pts AS (
  SELECT series_hash, ts, start_ts, temporality, bucket_bounds, %s AS grp,
         coalesce(value_double, value_int::DOUBLE) AS v,
         "count"::BIGINT AS c, "sum" AS s, bucket_counts::BIGINT[] AS b
  FROM %s
  WHERE %s
),
lagged AS (
  SELECT *, lag(ts) OVER w AS prev_ts, lag(start_ts) OVER w AS prev_start, lag(v) OVER w AS prev_v,
         lag(c) OVER w AS prev_c, lag(s) OVER w AS prev_s, lag(b) OVER w AS prev_b,
         lag(bucket_bounds) OVER w AS prev_bounds
  FROM pts
  WINDOW w AS (PARTITION BY series_hash ORDER BY ts)
),
classified AS (
  SELECT *, temporality = 1 AS is_delta,
         prev_ts IS NOT NULL AND (start_ts IS DISTINCT FROM prev_start
           OR coalesce(v < prev_v, false) OR coalesce(c < prev_c, false)) AS is_reset,
         prev_ts IS NULL AND start_ts IS NOT NULL AND start_ts >= ? AS is_fresh,
         prev_ts IS NOT NULL AND bucket_bounds IS DISTINCT FROM prev_bounds AS bounds_changed
  FROM lagged
),
deltas AS (
  SELECT grp, series_hash, ts, bucket_bounds,
         CASE WHEN is_delta OR is_reset OR is_fresh THEN v ELSE v - prev_v END AS dv,
         CASE WHEN is_delta OR is_reset OR is_fresh THEN c ELSE c - prev_c END AS dc,
         CASE WHEN is_delta OR is_reset OR is_fresh THEN s ELSE s - prev_s END AS ds,
         CASE WHEN is_delta OR is_reset OR is_fresh THEN b
              WHEN bounds_changed THEN NULL
              ELSE list_transform(range(1, len(b) + 1), lambda i: b[i] - prev_b[i]) END AS db
  FROM classified
  WHERE (is_delta OR is_reset OR is_fresh OR prev_ts IS NOT NULL) AND ts >= ?
)`

// metricQuery is one request's resolved state, shared by the paths.
type metricQuery struct {
	p          MetricQueryParams
	kind       MetricKind
	agg        Aggregate
	stepUS     int64
	from       time.Time // the range start aligned down to the step
	readFrom   time.Time // from - metricLookback, where the source starts
	src        string
	srcArgs    []any
	filter     *filter
	groupExpr  string
	groupArgs  []any
	timestamps []time.Time
}

// MetricQuery returns a metric aggregated per step and group over the range.
// The range start is aligned down to the step, so every step is whole and
// steps line up across queries; the result's range reports the aligned
// start. A step's value is computed from the points whose ts falls in it.
// Steps with no data are nil, never zero.
func MetricQuery(ctx context.Context, snap *Snapshot, p MetricQueryParams) (MetricResult, error) {
	if p.Name == "" {
		return MetricResult{}, invalid("name is required")
	}
	if !p.Range.From.Before(p.Range.To) {
		return MetricResult{}, invalid("from must be before to")
	}
	step, err := ResolveStep(p.Range, p.Step)
	if err != nil {
		return MetricResult{}, err
	}

	q := &metricQuery{p: p, stepUS: step.Microseconds()}
	q.from = time.UnixMicro(p.Range.From.UnixMicro() / q.stepUS * q.stepUS).UTC()
	q.readFrom = q.from.Add(-metricLookback)
	for t := q.from; t.Before(p.Range.To); t = t.Add(step) {
		q.timestamps = append(q.timestamps, t)
	}
	// One Source call: the kind lookup and the data query must agree.
	q.src, q.srcArgs, err = snap.Source(ctx, "metric_points", q.readFrom, p.Range.To)
	if err != nil {
		return MetricResult{}, err
	}
	q.filter = pointFilter(p.ProjectID, p.Service, p.Environment)
	q.filter.add("name = ?", p.Name)
	for _, a := range p.Attrs {
		q.filter.add("json_extract_string(attributes, ?) = ?", attrPath(a.Key), a.Value)
	}

	if q.kind, err = q.lookupKind(ctx, snap); err != nil {
		return MetricResult{}, err
	}
	if q.agg, err = ParseAggregate(q.kind, p.Aggregate); err != nil {
		return MetricResult{}, err
	}
	q.filter.add("type = ? AND monotonic = ? AND coalesce(unit, '') = ?", typeCode(q.kind), q.kind.Monotonic, q.kind.Unit)
	q.groupExpr, q.groupArgs = groupExpression(p.GroupBy)

	var groups map[string]*groupValues
	switch {
	case q.kind.isCounter():
		groups, err = q.counter(ctx, snap)
	case q.kind.Type == KindHistogram && q.agg.Quantile > 0:
		groups, err = q.quantiles(ctx, snap)
	case q.kind.Type == KindHistogram:
		groups, err = q.histogram(ctx, snap)
	default:
		groups, err = q.gauge(ctx, snap)
	}
	if err != nil {
		return MetricResult{}, err
	}

	res := MetricResult{
		Name: p.Name, MetricKind: q.kind, Aggregate: q.agg.Name, GroupBy: p.GroupBy,
		StepSeconds: q.stepUS / 1e6, Range: Range{From: q.from, To: p.Range.To},
		Timestamps: q.timestamps,
	}
	res.Series, res.Truncated = q.rank(groups)
	return res, nil
}

const kindsSQL = `SELECT DISTINCT type, monotonic, coalesce(unit, '') FROM %s WHERE %s AND ts >= ? ORDER BY 1, 2, 3`

// lookupKind returns the one kind the name has in the range under every
// filter, or ErrMetricNotFound, or a validation error naming the kinds.
func (q *metricQuery) lookupKind(ctx context.Context, snap *Snapshot) (MetricKind, error) {
	args := append(append(append([]any{}, q.srcArgs...), q.filter.args...), q.from)
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(kindsSQL, q.src, q.filter.clause()), args...)
	if err != nil {
		return MetricKind{}, fmt.Errorf("metric kinds: %w", err)
	}
	defer rows.Close()
	var kinds []MetricKind
	for rows.Next() {
		var k MetricKind
		var typ int16
		if err := rows.Scan(&typ, &k.Monotonic, &k.Unit); err != nil {
			return MetricKind{}, fmt.Errorf("scan metric kind: %w", err)
		}
		k.Type = typeName(typ)
		kinds = append(kinds, k)
	}
	if err := rows.Err(); err != nil {
		return MetricKind{}, fmt.Errorf("metric kinds: %w", err)
	}
	switch len(kinds) {
	case 0:
		return MetricKind{}, ErrMetricNotFound
	case 1:
		return kinds[0], nil
	}
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.String()
	}
	return MetricKind{}, invalid("metric %s has several kinds in the range (%s); narrow it with a service filter",
		q.p.Name, strings.Join(names, "; "))
}

func typeCode(k MetricKind) int16 {
	switch k.Type {
	case KindGauge:
		return telemetry.TypeGauge
	case KindSum:
		return telemetry.TypeSum
	default:
		return telemetry.TypeHistogram
	}
}

// groupExpression is the SQL that labels a point's group, and its argument.
func groupExpression(groupBy string) (string, []any) {
	switch groupBy {
	case "":
		return "''", nil
	case "service":
		return "service", nil
	case "environment":
		return "coalesce(environment, '')", nil
	case "version":
		return "coalesce(version, '')", nil
	default:
		return "coalesce(json_extract_string(attributes, ?), '')", []any{attrPath(groupBy)}
	}
}

// deltas returns the WITH clause over the deltas and its arguments.
func (q *metricQuery) deltas() (string, []any) {
	args := append([]any{}, q.groupArgs...)
	args = append(args, q.srcArgs...)
	args = append(args, q.filter.args...)
	args = append(args, q.readFrom, q.from)
	return "WITH " + fmt.Sprintf(deltasCTE, q.groupExpr, q.src, q.filter.clause()), args
}

// groupValues is one group's value per step index (relative to q.from).
type groupValues struct {
	values       map[int]float64
	incompatible *int
}

func (q *metricQuery) slot(groups map[string]*groupValues, grp string) *groupValues {
	g, ok := groups[grp]
	if !ok {
		g = &groupValues{values: map[int]float64{}}
		groups[grp] = g
	}
	return g
}

// index turns an absolute step number into a position in the timestamps.
func (q *metricQuery) index(stepI int64) (int, bool) {
	i := int(stepI - q.from.UnixMicro()/q.stepUS)
	return i, i >= 0 && i < len(q.timestamps)
}

// collectSteps reads rows of (grp, step_i, value) into groups.
func (q *metricQuery) collectSteps(ctx context.Context, snap *Snapshot, query string, args []any) (map[string]*groupValues, error) {
	rows, err := snap.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("metric query %s: %w", q.p.Name, err)
	}
	defer rows.Close()
	groups := map[string]*groupValues{}
	for rows.Next() {
		var grp string
		var stepI int64
		var v sql.NullFloat64
		if err := rows.Scan(&grp, &stepI, &v); err != nil {
			return nil, fmt.Errorf("scan metric step: %w", err)
		}
		g := q.slot(groups, grp)
		if i, ok := q.index(stepI); ok && v.Valid {
			g.values[i] = v.Float64
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metric query %s: %w", q.p.Name, err)
	}
	return groups, nil
}

const counterSQL = `%s
SELECT grp, epoch_us(ts) // %d AS step_i, sum(dv) %s
FROM deltas
GROUP BY grp, step_i`

// counter is a monotonic sum's increase, or rate, per step.
func (q *metricQuery) counter(ctx context.Context, snap *Snapshot) (map[string]*groupValues, error) {
	with, args := q.deltas()
	return q.collectSteps(ctx, snap, fmt.Sprintf(counterSQL, with, q.stepUS, q.perSecond()), args)
}

// perSecond divides a step's sum by the step's length for rate.
func (q *metricQuery) perSecond() string {
	if q.agg.Name != AggRate {
		return ""
	}
	return fmt.Sprintf("/ %d", q.stepUS/1e6)
}

const histogramSQL = `%s
SELECT grp, epoch_us(ts) // %d AS step_i, %s
FROM deltas
GROUP BY grp, step_i`

// histogram is a histogram's observation rate, or average, per step. The
// average uses only points that carry a sum.
func (q *metricQuery) histogram(ctx context.Context, snap *Snapshot) (map[string]*groupValues, error) {
	with, args := q.deltas()
	expr := "sum(dc)::DOUBLE " + q.perSecond()
	if q.agg.Name == AggAvg {
		expr = "sum(ds) FILTER (WHERE ds IS NOT NULL) / nullif(sum(dc) FILTER (WHERE ds IS NOT NULL), 0)"
	}
	return q.collectSteps(ctx, snap, fmt.Sprintf(histogramSQL, with, q.stepUS, expr), args)
}

const gaugeSQL = `WITH pts AS (
  SELECT series_hash, ts, %s AS grp, coalesce(value_double, value_int::DOUBLE) AS v
  FROM %s
  WHERE %s AND ts >= ?
),
per_series AS (
  SELECT grp, series_hash, epoch_us(ts) // %d AS step_i, %s AS x
  FROM pts
  GROUP BY grp, series_hash, step_i
)
SELECT grp, step_i, %s(x) FROM per_series GROUP BY grp, step_i`

// gauge reduces each series within a step, then combines the series of a
// group: avg, min, and max with the same function; sum takes each series'
// last point and adds them.
func (q *metricQuery) gauge(ctx context.Context, snap *Snapshot) (map[string]*groupValues, error) {
	reduce := q.agg.Name + "(v)"
	if q.agg.Name == AggSum {
		reduce = "arg_max(v, ts)"
	}
	args := append([]any{}, q.groupArgs...)
	args = append(args, q.srcArgs...)
	args = append(args, q.filter.args...)
	args = append(args, q.from)
	query := fmt.Sprintf(gaugeSQL, q.groupExpr, q.src, q.filter.clause(), q.stepUS, reduce, q.agg.Name)
	return q.collectSteps(ctx, snap, query, args)
}

const bucketSumsSQL = `%s,
qd AS (
  SELECT grp, epoch_us(ts) // %d AS step_i, series_hash, bucket_bounds, db
  FROM deltas WHERE db IS NOT NULL AND bucket_bounds IS NOT NULL
),
elems AS (
  SELECT grp, step_i, bucket_bounds, unnest(db) AS n, generate_subscripts(db, 1) AS idx FROM qd
),
step_sums AS (
  SELECT grp, step_i, bucket_bounds, list(total ORDER BY idx) AS counts
  FROM (SELECT grp, step_i, bucket_bounds, idx, sum(n)::DOUBLE AS total FROM elems GROUP BY grp, step_i, bucket_bounds, idx)
  GROUP BY grp, step_i, bucket_bounds
),
bound_groups AS (
  SELECT grp, bucket_bounds, list(DISTINCT series_hash) AS series, sum(list_sum(db))::DOUBLE AS total
  FROM qd GROUP BY grp, bucket_bounds
)
SELECT s.grp, s.step_i, s.bucket_bounds, s.counts, b.series, b.total
FROM step_sums s JOIN bound_groups b ON s.grp = b.grp AND s.bucket_bounds = b.bucket_bounds`

// quantiles interpolates a quantile per step from the bucket deltas of each
// group's merged bounds (see mergedBounds); series with other bounds are
// counted, not merged.
func (q *metricQuery) quantiles(ctx context.Context, snap *Snapshot) (map[string]*groupValues, error) {
	with, args := q.deltas()
	rows, err := snap.QueryContext(ctx, fmt.Sprintf(bucketSumsSQL, with, q.stepUS), args...)
	if err != nil {
		return nil, fmt.Errorf("metric query %s: %w", q.p.Name, err)
	}
	defer rows.Close()

	type stepCounts struct {
		step   int64
		bounds string
		counts []float64
	}
	byGroup := map[string]map[string]bucketGroup{} // grp -> bounds key -> group
	var steps = map[string][]stepCounts{}
	for rows.Next() {
		var grp string
		var stepI int64
		var bounds, counts, series []any
		var total float64
		if err := rows.Scan(&grp, &stepI, &bounds, &counts, &series, &total); err != nil {
			return nil, fmt.Errorf("scan histogram step: %w", err)
		}
		b := floats(bounds)
		key := fmt.Sprint(b)
		if byGroup[grp] == nil {
			byGroup[grp] = map[string]bucketGroup{}
		}
		byGroup[grp][key] = bucketGroup{bounds: b, series: uints(series), total: total}
		steps[grp] = append(steps[grp], stepCounts{step: stepI, bounds: key, counts: floats(counts)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metric query %s: %w", q.p.Name, err)
	}

	groups := map[string]*groupValues{}
	for grp, bg := range byGroup {
		candidates := make([]bucketGroup, 0, len(bg))
		for _, g := range bg {
			candidates = append(candidates, g)
		}
		main, incompatible := mergedBounds(candidates)
		mainKey := fmt.Sprint(main.bounds)
		g := q.slot(groups, grp)
		g.incompatible = &incompatible
		for _, sc := range steps[grp] {
			if sc.bounds != mainKey {
				continue
			}
			i, ok := q.index(sc.step)
			if !ok {
				continue
			}
			if v, ok := Interpolate(main.bounds, sc.counts, q.agg.Quantile); ok {
				g.values[i] = v
			}
		}
	}
	return groups, nil
}

// rank turns groups into series ordered by the sum of their values, then by
// label, and caps them at MaxMetricSeries. An ungrouped query always has its
// one series, empty when the range holds points but no values.
func (q *metricQuery) rank(groups map[string]*groupValues) ([]MetricSeries, bool) {
	if q.p.GroupBy == "" {
		q.slot(groups, "")
	}
	type ranked struct {
		label string
		total float64
		g     *groupValues
	}
	all := make([]ranked, 0, len(groups))
	for label, g := range groups {
		var total float64
		for _, v := range g.values {
			total += v
		}
		all = append(all, ranked{label, total, g})
	}
	slices.SortFunc(all, func(a, b ranked) int {
		if c := cmp.Compare(b.total, a.total); c != 0 {
			return c
		}
		return cmp.Compare(a.label, b.label)
	})
	truncated := len(all) > MaxMetricSeries
	if truncated {
		all = all[:MaxMetricSeries]
	}
	out := make([]MetricSeries, len(all))
	for i, r := range all {
		s := MetricSeries{Group: map[string]string{}, Values: make([]*float64, len(q.timestamps))}
		if q.p.GroupBy != "" {
			s.Group[q.p.GroupBy] = r.label
		}
		for idx, v := range r.g.values {
			s.Values[idx] = &v
		}
		s.IncompatibleSeries = r.g.incompatible
		out[i] = s
	}
	return out, truncated
}

// floats converts a scanned DuckDB list of numbers.
func floats(xs []any) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		switch v := x.(type) {
		case float64:
			out[i] = v
		case int64:
			out[i] = float64(v)
		case uint64:
			out[i] = float64(v)
		case int32:
			out[i] = float64(v)
		}
	}
	return out
}

// uints converts a scanned DuckDB list of UBIGINT.
func uints(xs []any) []uint64 {
	out := make([]uint64, len(xs))
	for i, x := range xs {
		if v, ok := x.(uint64); ok {
			out[i] = v
		}
	}
	return out
}
