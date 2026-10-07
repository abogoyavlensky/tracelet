package query

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"
)

// DefaultInterval is the generator's emission interval, the default spacing
// metric queries assume when looking back for each window's first delta.
const DefaultInterval = 10 * time.Second

// deltasCTE turns points into per-point deltas. Per series ordered by ts, a
// cumulative point is a reset when its value or count drops or its start_ts
// changes; a reset's delta is its own value, otherwise the difference from
// the previous point (element-wise for buckets). A cumulative point with no
// previous point has no delta and is dropped. Delta-temporality points are
// used as they are. The caller's source starts at from - 2 * interval so the
// window's first point has a predecessor; only deltas at or after from are
// kept (the last placeholder).
//
// Counts are cast to BIGINT before subtracting so UBIGINT never underflows.
const deltasCTE = `pts AS (
  SELECT series_hash, ts, start_ts, temporality, bucket_bounds,
         coalesce(value_double, value_int::DOUBLE) AS v,
         count::BIGINT AS c,
         bucket_counts::BIGINT[] AS b
  FROM %s
  WHERE name = ?
),
lagged AS (
  SELECT *,
         lag(ts) OVER w AS prev_ts,
         lag(start_ts) OVER w AS prev_start,
         lag(v) OVER w AS prev_v,
         lag(c) OVER w AS prev_c,
         lag(b) OVER w AS prev_b
  FROM pts
  WINDOW w AS (PARTITION BY series_hash ORDER BY ts)
),
classified AS (
  SELECT *,
         temporality = 1 AS is_delta,
         coalesce(start_ts IS DISTINCT FROM prev_start, false)
           OR coalesce(v < prev_v, false)
           OR coalesce(c < prev_c, false) AS is_reset
  FROM lagged
),
deltas AS (
  SELECT series_hash, ts, bucket_bounds,
         CASE WHEN is_delta OR is_reset THEN v ELSE v - prev_v END AS dv,
         CASE WHEN is_delta OR is_reset THEN c ELSE c - prev_c END AS dc,
         CASE WHEN is_delta OR is_reset THEN b
              ELSE list_transform(range(1, len(b) + 1), lambda i: b[i] - prev_b[i]) END AS db
  FROM classified
  WHERE (is_delta OR prev_ts IS NOT NULL) AND ts >= ?
)`

// deltasQuery returns the WITH clause over a signal source and the arguments
// for its placeholders, in order.
func (r Reader) deltasQuery(ctx context.Context, name string, from, to time.Time, interval time.Duration) (string, []any, error) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	src, args, err := r.source(ctx, "metric_points", from.Add(-2*interval), to)
	if err != nil {
		return "", nil, err
	}
	return "WITH " + fmt.Sprintf(deltasCTE, src), append(args, name, from), nil
}

// RateStep is one step of a counter rate: the summed delta in [Start, Start+step).
type RateStep struct {
	Start time.Time
	Delta float64
}

// Rate is a counter's increase over a window.
type Rate struct {
	Steps     []RateStep
	Total     float64
	PerSecond float64 // Total divided by the window length
}

const rateSQL = `%s
SELECT time_bucket(to_microseconds(?), ts) AS step, sum(%s)
FROM deltas
GROUP BY step
ORDER BY step`

// CounterRate sums a cumulative or delta sum's increases per step over
// [from, to).
func (r Reader) CounterRate(ctx context.Context, name string, from, to time.Time, step, interval time.Duration) (Rate, error) {
	return r.rate(ctx, "dv", name, from, to, step, interval)
}

// RequestRate sums a histogram's count increases per step over [from, to).
func (r Reader) RequestRate(ctx context.Context, name string, from, to time.Time, step, interval time.Duration) (Rate, error) {
	return r.rate(ctx, "dc::DOUBLE", name, from, to, step, interval)
}

func (r Reader) rate(ctx context.Context, delta, name string, from, to time.Time, step, interval time.Duration) (Rate, error) {
	with, args, err := r.deltasQuery(ctx, name, from, to, interval)
	if err != nil {
		return Rate{}, err
	}
	rows, err := r.DB.QueryContext(ctx, fmt.Sprintf(rateSQL, with, delta), append(args, step.Microseconds())...)
	if err != nil {
		return Rate{}, fmt.Errorf("rate %s: %w", name, err)
	}
	steps, err := collect(rows, func(rows *sql.Rows) (RateStep, error) {
		var s RateStep
		var d sql.NullFloat64
		err := rows.Scan(&s.Start, &d)
		s.Start, s.Delta = s.Start.UTC(), d.Float64
		return s, err
	})
	if err != nil {
		return Rate{}, err
	}

	rate := Rate{Steps: steps}
	for _, s := range steps {
		rate.Total += s.Delta
	}
	if window := to.Sub(from).Seconds(); window > 0 {
		rate.PerSecond = rate.Total / window
	}
	return rate, nil
}

// Quantile is a histogram quantile over a window.
type Quantile struct {
	Value float64
	OK    bool // false when the window holds no observations
	// IncompatibleSeries lists series whose bucket bounds differ from the
	// merged ones; they are reported, not merged.
	IncompatibleSeries []uint64
}

const bucketSumsSQL = `%s,
groups AS (
  SELECT bucket_bounds, list(DISTINCT series_hash ORDER BY series_hash) AS series
  FROM deltas
  GROUP BY bucket_bounds
),
sums AS (
  SELECT bucket_bounds, list(total ORDER BY idx) AS counts
  FROM (
    SELECT bucket_bounds, idx, sum(n)::DOUBLE AS total
    FROM (SELECT bucket_bounds, unnest(db) AS n, generate_subscripts(db, 1) AS idx FROM deltas)
    GROUP BY bucket_bounds, idx
  )
  GROUP BY bucket_bounds
)
SELECT g.bucket_bounds, g.series, s.counts
FROM groups g JOIN sums s ON g.bucket_bounds = s.bucket_bounds`

type bucketGroup struct {
	bounds []float64
	series []uint64
	counts []float64
}

// HistogramQuantile merges the delta buckets of every series of a histogram
// metric that shares the most common bounds and interpolates the q-quantile.
func (r Reader) HistogramQuantile(ctx context.Context, name string, q float64, from, to time.Time, interval time.Duration) (Quantile, error) {
	with, args, err := r.deltasQuery(ctx, name, from, to, interval)
	if err != nil {
		return Quantile{}, err
	}
	rows, err := r.DB.QueryContext(ctx, fmt.Sprintf(bucketSumsSQL, with), args...)
	if err != nil {
		return Quantile{}, fmt.Errorf("histogram %s: %w", name, err)
	}
	groups, err := collect(rows, func(rows *sql.Rows) (bucketGroup, error) {
		var bounds, series, counts []any
		if err := rows.Scan(&bounds, &series, &counts); err != nil {
			return bucketGroup{}, err
		}
		return bucketGroup{bounds: floats(bounds), series: uints(series), counts: floats(counts)}, nil
	})
	if err != nil {
		return Quantile{}, err
	}
	if len(groups) == 0 {
		return Quantile{}, nil
	}

	// The merged group is the one most series share; ties go to the larger
	// total so the choice is stable.
	slices.SortStableFunc(groups, func(a, b bucketGroup) int {
		if len(a.series) != len(b.series) {
			return len(b.series) - len(a.series)
		}
		return cmp.Compare(sum(b.counts), sum(a.counts))
	})
	main := groups[0]
	var res Quantile
	for _, g := range groups[1:] {
		res.IncompatibleSeries = append(res.IncompatibleSeries, g.series...)
	}
	slices.Sort(res.IncompatibleSeries)
	res.Value, res.OK = Interpolate(main.bounds, main.counts, q)
	return res, nil
}

// Interpolate returns the q-quantile of an explicit-bucket histogram. counts
// has one more element than bounds; bucket i covers (bounds[i-1], bounds[i]],
// the first starts at 0 (or at its bound if that is not positive), and the
// last is unbounded. A rank in the unbounded bucket returns the last finite
// bound, as Prometheus does. ok is false for an empty histogram.
func Interpolate(bounds, counts []float64, q float64) (float64, bool) {
	total := sum(counts)
	if total <= 0 || len(counts) != len(bounds)+1 {
		return 0, false
	}
	rank := q * total
	var before float64
	for i, n := range counts {
		if n <= 0 || before+n < rank {
			before += n
			continue
		}
		if i == len(bounds) {
			return bounds[len(bounds)-1], true
		}
		upper := bounds[i]
		lower := 0.0
		if i > 0 {
			lower = bounds[i-1]
		} else if upper <= 0 {
			return upper, true
		}
		return lower + (upper-lower)*(rank-before)/n, true
	}
	return bounds[len(bounds)-1], true
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
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
