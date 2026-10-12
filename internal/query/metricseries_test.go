package query_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// The metric fixtures sit in hour 1; the storage spike's tests used the same
// shapes from its start time.
var mt0 = h1

func minute(i int) time.Time { return mt0.Add(time.Duration(i) * time.Minute) }

// storages runs a test against hot rows and against the same rows flushed.
func storages(t *testing.T, test func(t *testing.T, cold bool)) {
	t.Helper()
	for _, cold := range []bool{false, true} {
		name := "hot"
		if cold {
			name = "cold"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			test(t, cold)
		})
	}
}

func withPoints(t *testing.T, cold bool, points ...telemetry.MetricPoint) *query.Snapshot {
	t.Helper()
	e := newEnv(t)
	writePoints(t, e, cold, points...)
	return e.open(t)
}

func counterPoint(hash uint64, ts, start time.Time, value float64, asInt bool) telemetry.MetricPoint {
	p := telemetry.MetricPoint{
		TS: ts, StartTS: start, Project: "p1", Service: "svc-01", Environment: "prod",
		Name: "jobs", Type: telemetry.TypeSum, Temporality: telemetry.TemporalityCumulative,
		Monotonic: true, SeriesHash: hash,
	}
	if asInt {
		p.ValueInt = new(int64(value))
	} else {
		p.ValueDouble = new(value)
	}
	return p
}

func histPoint(hash uint64, ts time.Time, bounds []float64, buckets []uint64) telemetry.MetricPoint {
	var count uint64
	for _, b := range buckets {
		count += b
	}
	return telemetry.MetricPoint{
		TS: ts, StartTS: mt0.Add(-time.Hour), Project: "p1", Service: "svc-01", Environment: "prod",
		Name: "duration", Type: telemetry.TypeHistogram, Temporality: telemetry.TemporalityCumulative,
		SeriesHash: hash, Count: &count, BucketCounts: buckets, BucketBounds: bounds,
	}
}

func gaugePoint(hash uint64, ts time.Time, v float64, attrs string) telemetry.MetricPoint {
	return telemetry.MetricPoint{
		TS: ts, Project: "p1", Service: "svc-01", Environment: "prod", Name: "depth",
		Type: telemetry.TypeGauge, SeriesHash: hash, ValueDouble: &v, Attributes: attrs,
	}
}

func metricQuery(t *testing.T, snap *query.Snapshot, p query.MetricQueryParams) query.MetricResult {
	t.Helper()
	if p.ProjectID == "" {
		p.ProjectID = "p1"
	}
	if p.Step == 0 {
		p.Step = time.Minute
	}
	res, err := query.MetricQuery(t.Context(), snap, p)
	require.NoError(t, err)
	return res
}

// values reads a series as numbers, with nil for empty steps.
func values(s query.MetricSeries) []any {
	out := make([]any, len(s.Values))
	for i, v := range s.Values {
		if v != nil {
			out[i] = *v
		}
	}
	return out
}

func assertValues(t *testing.T, want []any, s query.MetricSeries, msg ...any) {
	t.Helper()
	got := values(s)
	require.Len(t, got, len(want), msg...)
	for i := range want {
		if want[i] == nil {
			assert.Nil(t, got[i], append([]any{"step %d"}, i)...)
			continue
		}
		require.NotNil(t, got[i], "step %d", i)
		assert.InDelta(t, want[i], got[i], 1e-9, "step %d", i)
	}
}

func byGroup(t *testing.T, res query.MetricResult, key string) map[string]query.MetricSeries {
	t.Helper()
	out := map[string]query.MetricSeries{}
	for _, s := range res.Series {
		out[s.Group[key]] = s
	}
	return out
}

// Ported from the storage spike: the drop to 5 is a reset whose delta is 5.
func TestCounterRateWithResets(t *testing.T) {
	t.Parallel()
	for _, asInt := range []bool{false, true} {
		t.Run(fmt.Sprintf("int=%v", asInt), func(t *testing.T) {
			t.Parallel()
			storages(t, func(t *testing.T, cold bool) {
				var points []telemetry.MetricPoint
				for i, v := range []float64{10, 20, 30, 5, 15} {
					points = append(points, counterPoint(1, minute(i), mt0, v, asInt))
				}
				snap := withPoints(t, cold, points...)

				// The window starts at the second point; the first only seeds it.
				inc := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(5)}})
				assert.Equal(t, []time.Time{minute(1), minute(2), minute(3), minute(4)}, inc.Timestamps)
				require.Len(t, inc.Series, 1)
				assertValues(t, []any{10.0, 10.0, 5.0, 10.0}, inc.Series[0])

				rate := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Range: query.Range{From: minute(1), To: minute(5)}})
				assert.Equal(t, "rate", rate.Aggregate)
				assert.Equal(t, int64(60), rate.StepSeconds)
				assertValues(t, []any{10.0 / 60, 10.0 / 60, 5.0 / 60, 10.0 / 60}, rate.Series[0])
			})
		})
	}
}

// Ported: series 2 restarts at its third point without its value dropping;
// the changed start_ts alone makes it a reset, so its delta is 120.
func TestCounterRateStartTSResetAndSum(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		var points []telemetry.MetricPoint
		for i, v := range []float64{10, 20, 30, 5, 15} {
			points = append(points, counterPoint(1, minute(i), mt0, v, true))
		}
		restart := minute(2).Add(-time.Second)
		for i, v := range []float64{100, 110, 120, 130, 140} {
			start := mt0
			if i >= 2 {
				start = restart
			}
			points = append(points, counterPoint(2, minute(i), start, v, true))
		}
		snap := withPoints(t, cold, points...)

		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(5)}})
		assertValues(t, []any{20.0, 130.0, 15.0, 20.0}, res.Series[0])
	})
}

var bounds = []float64{10, 50, 100}

func histogramPoints() []telemetry.MetricPoint {
	return []telemetry.MetricPoint{
		// Deltas [2, 6, 2, 0] and [0, 4, 4, 2]; merged [2, 10, 6, 2], total 20.
		histPoint(1, minute(0), bounds, []uint64{1, 1, 1, 1}),
		histPoint(1, minute(1), bounds, []uint64{3, 7, 3, 1}),
		histPoint(2, minute(0), bounds, []uint64{5, 5, 5, 5}),
		histPoint(2, minute(1), bounds, []uint64{5, 9, 9, 7}),
		// Different bounds: reported, not merged.
		histPoint(3, minute(0), []float64{5, 10}, []uint64{1, 1, 1}),
		histPoint(3, minute(1), []float64{5, 10}, []uint64{2, 2, 2}),
	}
}

// Ported: merged buckets, interpolation, and incompatible bounds.
func TestHistogramQuantile(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold, histogramPoints()...)
		window := query.Range{From: minute(1), To: minute(2)}
		for _, tc := range []struct {
			agg  string
			want float64
		}{
			{"p50", 42},  // rank 10 in (10, 50]: 10 + 40 * (10 - 2) / 10
			{"p90", 100}, // rank 18 in (50, 100]: 50 + 50 * (18 - 12) / 6
			{"p99", 100}, // rank 19.8 in (100, +Inf): the last finite bound
		} {
			res := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: tc.agg, Range: window})
			require.Len(t, res.Series, 1)
			assertValues(t, []any{tc.want}, res.Series[0], tc.agg)
			require.NotNil(t, res.Series[0].IncompatibleSeries)
			assert.Equal(t, 1, *res.Series[0].IncompatibleSeries)
		}

		def := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Range: window})
		assert.Equal(t, "p95", def.Aggregate)

		_, err := query.MetricQuery(t.Context(), snap, query.MetricQueryParams{
			ProjectID: "p1", Name: "duration", Step: time.Minute, Range: query.Range{From: minute(30), To: minute(31)},
		})
		assert.ErrorIs(t, err, query.ErrMetricNotFound, "no points at all")
	})
}

// A window whose only point is the first of a long-running series has no
// delta: the series is there, every step nil.
func TestMetricWindowWithoutDeltas(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold, counterPoint(1, minute(40), mt0.Add(-time.Hour), 50, true))
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Range: query.Range{From: minute(40), To: minute(42)}})
		require.Len(t, res.Series, 1)
		assertValues(t, []any{nil, nil}, res.Series[0])
	})
}

// Ported from the spike's RequestRate: count deltas 14-4, 30-20, 6-3.
func TestHistogramRate(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold, histogramPoints()...)
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "rate", Range: query.Range{From: minute(1), To: minute(2)}})
		assertValues(t, []any{23.0 / 60}, res.Series[0])
		assert.Nil(t, res.Series[0].IncompatibleSeries)
	})
}

// Ported, with one change: the re-bucketed point's count delta (14 - 3)
// counts, because the count does not depend on the buckets; its bucket delta
// is still never subtracted.
func TestHistogramBoundsChangeIsNotSubtracted(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold,
			histPoint(1, minute(0), []float64{5, 10}, []uint64{1, 1, 1}),
			histPoint(1, minute(1), bounds, []uint64{3, 7, 3, 1}), // re-bucketed
			histPoint(1, minute(2), bounds, []uint64{5, 9, 9, 7}), // delta [2, 2, 6, 6]
		)
		window := query.Range{From: minute(1), To: minute(3)}

		rate := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "rate", Range: window})
		assertValues(t, []any{11.0 / 60, 16.0 / 60}, rate.Series[0])

		p50 := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "p50", Range: window})
		// rank 8 in (50, 100] with 4 before and 6 in it.
		assertValues(t, []any{nil, 50 + 50*4.0/6}, p50.Series[0])
		assert.Equal(t, 0, *p50.Series[0].IncompatibleSeries)
	})
}

// A cumulative histogram whose count drops and whose bounds change at the
// same point restarted from zero: its own buckets are the delta.
func TestHistogramResetWithNewBounds(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold,
			histPoint(1, minute(0), []float64{5, 10}, []uint64{5, 5, 5}),
			histPoint(1, minute(1), bounds, []uint64{1, 1, 0, 0}),
		)
		window := query.Range{From: minute(1), To: minute(2)}
		rate := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "rate", Range: window})
		assertValues(t, []any{2.0 / 60}, rate.Series[0])
		p50 := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "p50", Range: window})
		assertValues(t, []any{10.0}, p50.Series[0]) // rank 1 at the top of (0, 10]
	})
}

func TestHistogramAvg(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		withSum := func(ts time.Time, count uint64, sum float64) telemetry.MetricPoint {
			p := histPoint(1, ts, bounds, []uint64{count, 0, 0, 0})
			p.Sum = &sum
			return p
		}
		snap := withPoints(t, cold, withSum(minute(0), 4, 10), withSum(minute(1), 10, 40))
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "duration", Aggregate: "avg", Range: query.Range{From: minute(1), To: minute(2)}})
		assertValues(t, []any{5.0}, res.Series[0]) // (40 - 10) / (10 - 4)
	})
}

// Two replicas of one counter, interleaved: each is its own series, so there
// are no false resets and the increases add up.
func TestCounterReplicasDoNotCollide(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		old := mt0.Add(-time.Hour)
		var points []telemetry.MetricPoint
		for i := range 5 {
			points = append(points,
				counterPoint(1, minute(i), old, float64(100+10*i), true),
				counterPoint(2, minute(i).Add(30*time.Second), old, float64(1000+5*i), true))
		}
		snap := withPoints(t, cold, points...)
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(5)}})
		assertValues(t, []any{15.0, 15.0, 15.0, 15.0}, res.Series[0])
	})
}

// A series that started inside the rows read counts its first value; one
// that started long before does not.
func TestCounterFirstPointOfNewSeries(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold,
			counterPoint(1, minute(2), minute(1).Add(30*time.Second), 7, true),
			counterPoint(2, minute(2), mt0.Add(-time.Hour), 50, true),
			counterPoint(2, minute(3), mt0.Add(-time.Hour), 60, true),
		)
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(4)}})
		assertValues(t, []any{nil, 7.0, 10.0}, res.Series[0])
	})
}

func TestCounterDeltaTemporality(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		var points []telemetry.MetricPoint
		for i, v := range []float64{3, 4, 5} {
			p := counterPoint(1, minute(i+1), minute(i), v, true)
			p.Temporality = telemetry.TemporalityDelta
			points = append(points, p)
		}
		snap := withPoints(t, cold, points...)
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(4)}})
		assertValues(t, []any{3.0, 4.0, 5.0}, res.Series[0])
	})
}

func TestGaugeAggregates(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		snap := withPoints(t, cold,
			gaugePoint(10, minute(0), 1, ""),
			gaugePoint(10, minute(0).Add(30*time.Second), 3, ""),
			gaugePoint(10, minute(1), 5, ""),
			gaugePoint(11, minute(0), 10, ""),
			gaugePoint(11, minute(1).Add(30*time.Second), 20, ""),
		)
		window := query.Range{From: minute(0), To: minute(3)}
		for agg, want := range map[string][]any{
			"avg": {6.0, 12.5, nil}, // series averages 2 and 10, then 5 and 20
			"min": {1.0, 5.0, nil},
			"max": {10.0, 20.0, nil},
			"sum": {13.0, 25.0, nil}, // each series' last point: 3 + 10, 5 + 20
		} {
			res := metricQuery(t, snap, query.MetricQueryParams{Name: "depth", Aggregate: agg, Range: window})
			assertValues(t, want, res.Series[0], agg)
		}
		def := metricQuery(t, snap, query.MetricQueryParams{Name: "depth", Range: window})
		assert.Equal(t, "avg", def.Aggregate)
	})
}

func TestUpDownCounterIsAGauge(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		level := func(ts time.Time, v int64) telemetry.MetricPoint {
			p := counterPoint(1, ts, mt0.Add(-time.Hour), float64(v), true)
			p.Name, p.Monotonic = "connections", false
			return p
		}
		snap := withPoints(t, cold, level(minute(0), 4), level(minute(1), 6))
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "connections", Range: query.Range{From: minute(0), To: minute(2)}})
		assert.Equal(t, query.MetricKind{Type: query.KindSum}, res.MetricKind)
		assert.Equal(t, "avg", res.Aggregate)
		assertValues(t, []any{4.0, 6.0}, res.Series[0])
	})
}

func TestGroupBy(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		old := mt0.Add(-time.Hour)
		series := func(hash uint64, service, attrs string, step float64) []telemetry.MetricPoint {
			var out []telemetry.MetricPoint
			for i := range 3 {
				p := counterPoint(hash, minute(i), old, step*float64(i), true)
				p.Service, p.Attributes = service, attrs
				out = append(out, p)
			}
			return out
		}
		var points []telemetry.MetricPoint
		points = append(points, series(1, "api", `{"http.route":"/a"}`, 1)...)
		points = append(points, series(2, "worker", `{"http.route":"/b"}`, 10)...)
		points = append(points, series(3, "api", `{}`, 100)...)
		snap := withPoints(t, cold, points...)
		window := query.Range{From: minute(1), To: minute(3)}
		inc := func(p query.MetricQueryParams) query.MetricResult {
			p.Name, p.Aggregate, p.Range = "jobs", "increase", window
			return metricQuery(t, snap, p)
		}

		bySvc := byGroup(t, inc(query.MetricQueryParams{GroupBy: "service"}), "service")
		assertValues(t, []any{101.0, 101.0}, bySvc["api"])
		assertValues(t, []any{10.0, 10.0}, bySvc["worker"])

		res := inc(query.MetricQueryParams{GroupBy: "http.route"})
		assert.Equal(t, "", res.Series[0].Group["http.route"], "largest first: the series without the attribute")
		byRoute := byGroup(t, res, "http.route")
		assert.Len(t, byRoute, 3)
		assertValues(t, []any{1.0, 1.0}, byRoute["/a"])
		assertValues(t, []any{100.0, 100.0}, byRoute[""])

		// A service filter, an attribute group, and an attribute filter
		// together exercise the placeholder order.
		apiRoutes := byGroup(t, inc(query.MetricQueryParams{GroupBy: "http.route", Service: "api"}), "http.route")
		assert.Len(t, apiRoutes, 2)
		filtered := inc(query.MetricQueryParams{GroupBy: "service", Attrs: []query.Attr{{Key: "http.route", Value: "/a"}}})
		require.Len(t, filtered.Series, 1)
		assert.Equal(t, map[string]string{"service": "api"}, filtered.Series[0].Group)
		assertValues(t, []any{1.0, 1.0}, filtered.Series[0])

		plain := inc(query.MetricQueryParams{})
		require.Len(t, plain.Series, 1)
		assert.Empty(t, plain.Series[0].Group)
		assertValues(t, []any{111.0, 111.0}, plain.Series[0])
	})
}

func TestSeriesCapAndRanking(t *testing.T) {
	t.Parallel()
	var points []telemetry.MetricPoint
	for i := range 25 {
		points = append(points, gaugePoint(uint64(100+i), minute(0), float64(i), fmt.Sprintf(`{"k":"%02d"}`, i)))
	}
	snap := withPoints(t, false, points...)
	res := metricQuery(t, snap, query.MetricQueryParams{Name: "depth", GroupBy: "k", Range: query.Range{From: minute(0), To: minute(1)}})
	require.Len(t, res.Series, query.MaxMetricSeries)
	assert.True(t, res.Truncated)
	assert.Equal(t, "24", res.Series[0].Group["k"])
	assert.Equal(t, "05", res.Series[19].Group["k"])
}

func TestTimestampsAligned(t *testing.T) {
	t.Parallel()
	var points []telemetry.MetricPoint
	for i := range 6 {
		points = append(points, counterPoint(1, minute(i), mt0.Add(-time.Hour), float64(60*i), true))
	}
	snap := withPoints(t, false, points...)
	want := []time.Time{minute(1), minute(2), minute(3), minute(4)}
	for _, from := range []time.Time{minute(1).Add(20 * time.Second), minute(1).Add(40 * time.Second)} {
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Range: query.Range{From: from, To: minute(4).Add(10 * time.Second)}})
		assert.Equal(t, want, res.Timestamps)
		assert.Equal(t, query.Range{From: minute(1), To: minute(4).Add(10 * time.Second)}, res.Range)
		// A steady counter on step boundaries: every full step is 1/s.
		assertValues(t, []any{1.0, 1.0, 1.0, 1.0}, res.Series[0])
	}
}

func TestMetricQueryErrors(t *testing.T) {
	t.Parallel()
	gauge := gaugePoint(1, minute(0), 1, "")
	gauge.Name, gauge.Service = "jobs", "a"
	counter := counterPoint(2, minute(0), mt0, 1, true)
	counter.Service = "b"
	snap := withPoints(t, false, gauge, counter)
	window := query.Range{From: minute(0), To: minute(1)}
	run := func(p query.MetricQueryParams) error {
		p.ProjectID, p.Range = "p1", window
		_, err := query.MetricQuery(t.Context(), snap, p)
		return err
	}

	assert.ErrorIs(t, run(query.MetricQueryParams{Name: "missing"}), query.ErrMetricNotFound)

	var verr *query.ValidationError
	err := run(query.MetricQueryParams{Name: "jobs"})
	require.ErrorAs(t, err, &verr)
	assert.Contains(t, err.Error(), "several kinds")
	assert.Contains(t, err.Error(), "gauge; monotonic sum")

	require.NoError(t, run(query.MetricQueryParams{Name: "jobs", Service: "a"}), "a filter disambiguates")
	assert.ErrorAs(t, run(query.MetricQueryParams{Name: "jobs", Service: "a", Aggregate: "rate"}), &verr)
	assert.ErrorAs(t, run(query.MetricQueryParams{}), &verr, "no name")
	assert.ErrorAs(t, run(query.MetricQueryParams{Name: "jobs", Service: "a", Step: time.Second}), &verr, "bad step")
}

// Integer counters are subtracted as integers: past 2^53 a double cannot
// tell consecutive values apart.
func TestCounterIntegerPrecision(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		old := mt0.Add(-time.Hour)
		big := func(ts time.Time, v int64) telemetry.MetricPoint {
			p := counterPoint(1, ts, old, 0, true)
			p.ValueInt = &v
			return p
		}
		snap := withPoints(t, cold, big(minute(0), 1<<53), big(minute(1), 1<<53+1), big(minute(2), 1<<53+3))
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", Range: query.Range{From: minute(1), To: minute(3)}})
		assertValues(t, []any{1.0, 2.0}, res.Series[0])
	})
}

// A group whose only point has no predecessor still appears, all null.
func TestGroupWithoutDeltasIsKept(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		old := mt0.Add(-time.Hour)
		lone := counterPoint(2, minute(2), old, 50, true)
		lone.Service = "worker"
		snap := withPoints(t, cold,
			counterPoint(1, minute(1), old, 10, true),
			counterPoint(1, minute(2), old, 20, true),
			lone,
		)
		res := metricQuery(t, snap, query.MetricQueryParams{Name: "jobs", Aggregate: "increase", GroupBy: "service", Range: query.Range{From: minute(1), To: minute(3)}})
		groups := byGroup(t, res, "service")
		require.Len(t, groups, 2)
		assertValues(t, []any{nil, 10.0}, groups["svc-01"])
		assertValues(t, []any{nil, nil}, groups["worker"])
	})
}
