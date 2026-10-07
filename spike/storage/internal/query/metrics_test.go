package query_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/query"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/testenv"
)

var t0 = testenv.Start

func minute(i int) time.Time { return t0.Add(time.Duration(i) * time.Minute) }

// withPoints writes points and returns a reader over them, either all hot or,
// after FlushAll, all cold.
func withPoints(t *testing.T, cold bool, points []telemetry.MetricPoint) query.Reader {
	t.Helper()
	e := testenv.New(t)
	e.Write(t, telemetry.Batch{Points: points}, testenv.Counts{})
	if cold {
		_, err := e.Flusher.FlushAll(t.Context())
		require.NoError(t, err)
	}
	return query.Reader{DB: e.Store.DB(), Manifest: e.Manifest, DataDir: e.DataDir}
}

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

func counterPoint(hash uint64, i int, start time.Time, value float64, asInt bool) telemetry.MetricPoint {
	p := telemetry.MetricPoint{
		TS: minute(i), StartTS: start, Project: "default", Service: "svc-01",
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

func steps(r query.Rate) []float64 {
	var out []float64
	for _, s := range r.Steps {
		out = append(out, s.Delta)
	}
	return out
}

func TestCounterRateWithResets(t *testing.T) {
	t.Parallel()
	for _, asInt := range []bool{false, true} {
		name := "value_double"
		if asInt {
			name = "value_int"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			storages(t, func(t *testing.T, cold bool) {
				var points []telemetry.MetricPoint
				// Series 1: the drop to 5 is a reset whose delta is 5.
				for i, v := range []float64{10, 20, 30, 5, 15} {
					points = append(points, counterPoint(1, i, t0, v, asInt))
				}
				r := withPoints(t, cold, points)

				// The window starts at the second point; the first only seeds it.
				rate, err := r.CounterRate(t.Context(), "jobs", minute(1), minute(5), time.Minute, time.Minute)
				require.NoError(t, err)
				assert.Equal(t, []float64{10, 10, 5, 10}, steps(rate))
				assert.InDelta(t, 35, rate.Total, 1e-9)
				assert.InDelta(t, 35.0/240, rate.PerSecond, 1e-9)
				assert.Equal(t, minute(1), rate.Steps[0].Start)
			})
		})
	}
}

func TestCounterRateStartTSResetAndSum(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		var points []telemetry.MetricPoint
		for i, v := range []float64{10, 20, 30, 5, 15} {
			points = append(points, counterPoint(1, i, t0, v, true))
		}
		// Series 2 restarts at its third point without its value dropping:
		// the changed start_ts alone makes it a reset, so its delta is 120.
		restart := minute(2).Add(-time.Second)
		for i, v := range []float64{100, 110, 120, 130, 140} {
			start := t0
			if i >= 2 {
				start = restart
			}
			points = append(points, counterPoint(2, i, start, v, true))
		}
		r := withPoints(t, cold, points)

		rate, err := r.CounterRate(t.Context(), "jobs", minute(1), minute(5), time.Minute, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, []float64{20, 130, 15, 20}, steps(rate))
		assert.InDelta(t, 185, rate.Total, 1e-9)
	})
}

var bounds = []float64{10, 50, 100}

func histPoint(hash uint64, i int, bounds []float64, buckets []uint64) telemetry.MetricPoint {
	var count uint64
	for _, b := range buckets {
		count += b
	}
	return telemetry.MetricPoint{
		TS: minute(i), StartTS: t0.Add(-time.Hour), Project: "default", Service: "svc-01",
		Name: "duration", Type: telemetry.TypeHistogram, Temporality: telemetry.TemporalityCumulative,
		SeriesHash: hash, Count: &count, BucketCounts: buckets, BucketBounds: bounds,
	}
}

func histogramPoints() []telemetry.MetricPoint {
	return []telemetry.MetricPoint{
		// Deltas [2, 6, 2, 0] and [0, 4, 4, 2]; merged [2, 10, 6, 2], total 20.
		histPoint(1, 0, bounds, []uint64{1, 1, 1, 1}),
		histPoint(1, 1, bounds, []uint64{3, 7, 3, 1}),
		histPoint(2, 0, bounds, []uint64{5, 5, 5, 5}),
		histPoint(2, 1, bounds, []uint64{5, 9, 9, 7}),
		// Different bounds: reported, not merged.
		histPoint(3, 0, []float64{5, 10}, []uint64{1, 1, 1}),
		histPoint(3, 1, []float64{5, 10}, []uint64{2, 2, 2}),
	}
}

func TestHistogramQuantile(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		r := withPoints(t, cold, histogramPoints())
		ctx := t.Context()

		for _, tc := range []struct {
			q    float64
			want float64
		}{
			{0.50, 42},  // rank 10 in (10, 50]: 10 + 40 * (10 - 2) / 10
			{0.90, 100}, // rank 18 in (50, 100]: 50 + 50 * (18 - 12) / 6
			{0.99, 100}, // rank 19.8 in (100, +Inf): the last finite bound
		} {
			got, err := r.HistogramQuantile(ctx, "duration", tc.q, minute(1), minute(2), time.Minute)
			require.NoError(t, err)
			assert.True(t, got.OK)
			assert.InDelta(t, tc.want, got.Value, 1e-9, "q=%v", tc.q)
			assert.Equal(t, []uint64{3}, got.IncompatibleSeries)
		}

		empty, err := r.HistogramQuantile(ctx, "duration", 0.5, minute(30), minute(31), time.Minute)
		require.NoError(t, err)
		assert.False(t, empty.OK)
	})
}

func TestRequestRate(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		r := withPoints(t, cold, histogramPoints())

		rate, err := r.RequestRate(t.Context(), "duration", minute(1), minute(2), time.Minute, time.Minute)
		require.NoError(t, err)
		// Count deltas: 14-4, 30-20, 6-3.
		assert.Equal(t, []float64{23}, steps(rate))
	})
}

func TestInterpolateEdges(t *testing.T) {
	t.Parallel()
	_, ok := query.Interpolate(bounds, []float64{0, 0, 0, 0}, 0.5)
	assert.False(t, ok)

	_, ok = query.Interpolate(nil, []float64{5}, 0.5)
	assert.False(t, ok, "no finite bounds")

	v, ok := query.Interpolate(bounds, []float64{4, 0, 0, 0}, 0.5)
	assert.True(t, ok)
	assert.InDelta(t, 5, v, 1e-9, "first bucket interpolates from zero")
}

func TestHistogramBoundsChangeIsNotSubtracted(t *testing.T) {
	t.Parallel()
	storages(t, func(t *testing.T, cold bool) {
		r := withPoints(t, cold, []telemetry.MetricPoint{
			histPoint(1, 0, []float64{5, 10}, []uint64{1, 1, 1}),
			histPoint(1, 1, bounds, []uint64{3, 7, 3, 1}), // re-bucketed: seeds only
			histPoint(1, 2, bounds, []uint64{5, 9, 9, 7}), // delta [2, 2, 6, 6]
		})

		rate, err := r.RequestRate(t.Context(), "duration", minute(1), minute(3), time.Minute, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, []float64{16}, steps(rate))

		got, err := r.HistogramQuantile(t.Context(), "duration", 0.5, minute(1), minute(3), time.Minute)
		require.NoError(t, err)
		assert.True(t, got.OK)
		assert.Empty(t, got.IncompatibleSeries)
		// rank 8 in (50, 100] with 4 before and 6 in it.
		assert.InDelta(t, 50+50*4.0/6, got.Value, 1e-9)
	})
}
