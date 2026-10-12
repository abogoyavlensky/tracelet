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

// writePoints writes points and, when cold, flushes every hour before h2.
func writePoints(t *testing.T, e *env, cold bool, points ...telemetry.MetricPoint) {
	t.Helper()
	_, err := e.writer.Write(t.Context(), telemetry.Batch{Points: points})
	require.NoError(t, err)
	if cold {
		_, err = e.flusher.FlushDue(t.Context(), h2.Add(time.Hour))
		require.NoError(t, err)
	}
}

func gaugeAt(project, service, name string, hash uint64, ts time.Time, v float64, attrs string) telemetry.MetricPoint {
	return telemetry.MetricPoint{
		TS: ts, Project: project, Service: service, Environment: "prod", Name: name,
		Type: telemetry.TypeGauge, SeriesHash: hash, ValueDouble: &v, Attributes: attrs,
	}
}

func counterAt(project, service, name string, hash uint64, ts time.Time, v int64) telemetry.MetricPoint {
	return telemetry.MetricPoint{
		TS: ts, StartTS: h0.Add(-time.Hour), Project: project, Service: service, Environment: "staging",
		Name: name, Unit: "{order}", Type: telemetry.TypeSum, Temporality: telemetry.TemporalityCumulative,
		Monotonic: true, SeriesHash: hash, ValueInt: &v,
	}
}

// catalogEnv holds three metrics of p1 across two services, split over a
// flushed hour 0 and a hot hour 1, and one metric of p2.
func catalogEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	writePoints(t, e, true,
		gaugeAt("p1", "api", "queue.depth", 1, h0.Add(10*time.Minute), 3, `{"queue":"mail","http.route":"/a"}`),
		gaugeAt("p1", "worker", "queue.depth", 2, h0.Add(20*time.Minute), 5, `{"queue":"jobs"}`),
		counterAt("p1", "api", "Orders.Created", 3, h0.Add(30*time.Minute), 7),
	)
	writePoints(t, e, false,
		gaugeAt("p1", "api", "queue.depth", 1, h1.Add(10*time.Minute), 4, `{"queue":"mail","http.route":"/b"}`),
		counterAt("p1", "api", "Orders.Created", 3, h1.Add(30*time.Minute), 9),
		gaugeAt("p1", "api", "mem.used", 4, h1.Add(40*time.Minute), 1, ""),
		gaugeAt("p2", "api", "other.metric", 5, h1.Add(40*time.Minute), 1, ""),
	)
	hours, err := e.store.HotHours(t.Context(), "metric_points")
	require.NoError(t, err)
	require.Equal(t, []time.Time{h1}, hours, "hour 0 is cold, hour 1 hot")
	return e
}

func names(l query.MetricsList) []string {
	out := []string{}
	for _, m := range l.Items {
		out = append(out, m.Name)
	}
	return out
}

func TestListMetrics(t *testing.T) {
	e := catalogEnv(t)
	all := query.Range{From: h0, To: h2}
	// One snapshot for every query: each open snapshot holds a gate slot
	// until the test ends.
	snap := e.open(t)
	list := func(p query.MetricsParams) query.MetricsList {
		t.Helper()
		if p.ProjectID == "" {
			p.ProjectID = "p1"
		}
		if p.Range == (query.Range{}) {
			p.Range = all
		}
		got, err := query.ListMetrics(t.Context(), snap, p)
		require.NoError(t, err)
		return got
	}

	got := list(query.MetricsParams{})
	assert.Equal(t, []string{"Orders.Created", "mem.used", "queue.depth"}, names(got))
	assert.False(t, got.Truncated)
	assert.Equal(t, all, got.Range)

	orders := got.Items[0]
	assert.Equal(t, query.MetricKind{Type: query.KindSum, Monotonic: true, Unit: "{order}"}, orders.MetricKind)
	assert.Equal(t, int64(1), orders.Series)
	assert.Equal(t, int64(2), orders.Points)
	assert.Equal(t, h1.Add(30*time.Minute), orders.LastTS)

	depth := got.Items[2]
	assert.Equal(t, query.MetricKind{Type: query.KindGauge}, depth.MetricKind)
	assert.Equal(t, []string{"api", "worker"}, depth.Services)
	assert.Equal(t, int64(2), depth.Series)
	assert.Equal(t, int64(3), depth.Points)

	assert.Equal(t, []string{"Orders.Created", "mem.used", "queue.depth"}, names(list(query.MetricsParams{Service: "api"})))
	assert.Equal(t, []string{"queue.depth"}, names(list(query.MetricsParams{Service: "worker"})))
	assert.Equal(t, []string{"Orders.Created"}, names(list(query.MetricsParams{Environment: "staging"})))
	assert.Equal(t, []string{"Orders.Created"}, names(list(query.MetricsParams{Query: "orders"})), "case-insensitive")
	assert.Empty(t, names(list(query.MetricsParams{Query: "%"})), "wildcards are literal")
	assert.Equal(t, []string{"other.metric"}, names(list(query.MetricsParams{ProjectID: "p2"})))
	assert.Equal(t, []string{"queue.depth"}, names(list(query.MetricsParams{Range: query.Range{From: h0, To: h0.Add(25 * time.Minute)}})), "range")
}

func TestListMetricAttributes(t *testing.T) {
	e := catalogEnv(t)
	snap := e.open(t)
	got, err := query.ListMetricAttributes(t.Context(), snap, query.MetricAttributesParams{
		ProjectID: "p1", Name: "queue.depth", Range: query.Range{From: h0, To: h2},
	})
	require.NoError(t, err)
	assert.Equal(t, "queue.depth", got.Name)
	assert.Equal(t, []query.AttributeInfo{
		{Key: "http.route", Distinct: 2, Values: []string{"/a", "/b"}},
		{Key: "queue", Distinct: 2, Values: []string{"jobs", "mail"}},
	}, got.Items)

	got, err = query.ListMetricAttributes(t.Context(), snap, query.MetricAttributesParams{
		ProjectID: "p1", Name: "queue.depth", Service: "worker", Range: query.Range{From: h0, To: h2},
	})
	require.NoError(t, err)
	assert.Equal(t, []query.AttributeInfo{{Key: "queue", Distinct: 1, Values: []string{"jobs"}}}, got.Items)

	_, err = query.ListMetricAttributes(t.Context(), snap, query.MetricAttributesParams{
		ProjectID: "p1", Range: query.Range{From: h0, To: h2},
	})
	var verr *query.ValidationError
	assert.ErrorAs(t, err, &verr)
}

func TestListMetricAttributesCaps(t *testing.T) {
	e := newEnv(t)
	var points []telemetry.MetricPoint
	for i := range 120 {
		points = append(points, gaugeAt("p1", "api", "wide", uint64(i+1), h1.Add(time.Duration(i)*time.Second), 1,
			fmt.Sprintf(`{"k%03d":"v","shared":"v%03d"}`, i, i)))
	}
	writePoints(t, e, false, points...)
	got, err := query.ListMetricAttributes(t.Context(), e.open(t), query.MetricAttributesParams{
		ProjectID: "p1", Name: "wide", Range: query.Range{From: h0, To: h2},
	})
	require.NoError(t, err)
	require.Len(t, got.Items, query.MaxAttributeKeys)
	assert.Equal(t, "k000", got.Items[0].Key)
	assert.Equal(t, "k099", got.Items[99].Key, "shared sorts after the k keys, so it is past the cap")

	// Values are capped too: 120 distinct values of one key.
	var many []telemetry.MetricPoint
	for i := range 120 {
		many = append(many, gaugeAt("p1", "api", "many", uint64(1000+i), h1.Add(time.Duration(i)*time.Second), 1,
			fmt.Sprintf(`{"user":"u%03d"}`, i)))
	}
	writePoints(t, e, false, many...)
	got, err = query.ListMetricAttributes(t.Context(), e.open(t), query.MetricAttributesParams{
		ProjectID: "p1", Name: "many", Range: query.Range{From: h0, To: h2},
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	assert.Equal(t, int64(120), got.Items[0].Distinct)
	assert.Len(t, got.Items[0].Values, query.MaxAttributeValues)
	assert.Equal(t, "u000", got.Items[0].Values[0])

}

func TestParseAggregate(t *testing.T) {
	counter := query.MetricKind{Type: query.KindSum, Monotonic: true}
	updown := query.MetricKind{Type: query.KindSum}
	gauge := query.MetricKind{Type: query.KindGauge}
	hist := query.MetricKind{Type: query.KindHistogram, Unit: "s"}

	for _, tc := range []struct {
		kind     query.MetricKind
		in, name string
		q        float64
	}{
		{counter, "", "rate", 0},
		{counter, "increase", "increase", 0},
		{updown, "", "avg", 0},
		{gauge, "sum", "sum", 0},
		{gauge, "min", "min", 0},
		{hist, "", "p95", 0.95},
		{hist, "p50", "p50", 0.5},
		{hist, "p99.9", "p99.9", 0.999},
		{hist, "rate", "rate", 0},
		{hist, "avg", "avg", 0},
	} {
		got, err := query.ParseAggregate(tc.kind, tc.in)
		require.NoError(t, err, "%v %q", tc.kind, tc.in)
		assert.Equal(t, tc.name, got.Name)
		assert.InDelta(t, tc.q, got.Quantile, 1e-12)
	}

	for _, tc := range []struct {
		kind query.MetricKind
		in   string
	}{
		{gauge, "rate"}, {counter, "avg"}, {counter, "p95"}, {hist, "p0"}, {hist, "p100"},
		{hist, "p1000"}, {hist, "p99.99"}, {hist, "p"}, {hist, "sum"}, {gauge, "p50"},
	} {
		_, err := query.ParseAggregate(tc.kind, tc.in)
		var verr *query.ValidationError
		assert.ErrorAs(t, err, &verr, "%v %q", tc.kind, tc.in)
	}
	_, err := query.ParseAggregate(gauge, "rate")
	assert.ErrorContains(t, err, "avg, min, max, sum")
}

func TestResolveStep(t *testing.T) {
	rng := func(d time.Duration) query.Range { return query.Range{From: h0, To: h0.Add(d)} }
	for _, tc := range []struct {
		span, want time.Duration
	}{
		{time.Hour, time.Minute},
		{6 * time.Hour, 2 * time.Minute},
		{24 * time.Hour, 10 * time.Minute},
		{168 * time.Hour, time.Hour},
		{5 * time.Minute, time.Minute},
	} {
		got, err := query.ResolveStep(rng(tc.span), 0)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "auto step for %s", tc.span)
	}

	got, err := query.ResolveStep(rng(time.Hour), 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, got)

	var verr *query.ValidationError
	_, err = query.ResolveStep(rng(time.Hour), 5*time.Second)
	assert.ErrorAs(t, err, &verr, "under the minimum")
	_, err = query.ResolveStep(rng(168*time.Hour), 10*time.Second)
	assert.ErrorAs(t, err, &verr, "too many steps")
	_, err = query.ResolveStep(rng(time.Hour), 1500*time.Millisecond+10*time.Second)
	assert.ErrorAs(t, err, &verr, "fractional seconds")
}

func TestServicesCountsPoints(t *testing.T) {
	e := catalogEnv(t)
	got, err := query.Services(t.Context(), e.open(t), "p1", query.Range{From: h0, To: h2})
	require.NoError(t, err)
	assert.Equal(t, []query.ServiceCount{{Service: "api", Points: 5}, {Service: "worker", Points: 1}}, got)
}
