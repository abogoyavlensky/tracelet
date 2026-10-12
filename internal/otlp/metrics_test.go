package otlp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/otlp"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

var two = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 2}}

func intPoint(ts time.Time, v int64, attrs ...*commonpb.KeyValue) *metricspb.NumberDataPoint {
	return &metricspb.NumberDataPoint{
		TimeUnixNano: nanos(ts), StartTimeUnixNano: nanos(t0.Add(-time.Hour)),
		Value: &metricspb.NumberDataPoint_AsInt{AsInt: v}, Attributes: attrs,
	}
}

func doublePoint(ts time.Time, v float64, attrs ...*commonpb.KeyValue) *metricspb.NumberDataPoint {
	return &metricspb.NumberDataPoint{TimeUnixNano: nanos(ts), Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: v}, Attributes: attrs}
}

func sum(name string, temporality metricspb.AggregationTemporality, monotonic bool, points ...*metricspb.NumberDataPoint) *metricspb.Metric {
	return &metricspb.Metric{Name: name, Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
		AggregationTemporality: temporality, IsMonotonic: monotonic, DataPoints: points,
	}}}
}

const cumulative = metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE

func resourceMetrics(instance string, scope *commonpb.InstrumentationScope, metrics ...*metricspb.Metric) *metricspb.ResourceMetrics {
	return &metricspb.ResourceMetrics{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			kv("service.name", str("api")),
			kv("service.version", str("1.4.0")),
			kv("deployment.environment.name", str("prod")),
			kv("service.instance.id", str(instance)),
		}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Scope: scope, Metrics: metrics}},
	}
}

// metricsFixture is one resource's worth of every metric kind the tests
// check, valid and not.
func metricsFixture() []*metricspb.Metric {
	pSum, pMin, pMax := 2.75, 0.05, 1.5
	return []*metricspb.Metric{
		sum("orders.created", cumulative, true,
			intPoint(t0, 7, kv("plan", str("pro"))),
			intPoint(t0, 3, kv("plan", str("free")))),
		{Name: "queue.depth", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
			doublePoint(t0, 4.5),
			{TimeUnixNano: nanos(t0), Flags: uint32(metricspb.DataPointFlags_DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK)},
			{TimeUnixNano: nanos(t0)},                           // no value
			{Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1}}, // no time
		}}}},
		sum("connections.active", cumulative, false, intPoint(t0, 12)),
		{Name: "http.server.request.duration", Unit: "s", Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			AggregationTemporality: cumulative,
			DataPoints: []*metricspb.HistogramDataPoint{
				{
					TimeUnixNano: nanos(t0), StartTimeUnixNano: nanos(t0.Add(-time.Hour)),
					Count: 6, Sum: &pSum, Min: &pMin, Max: &pMax,
					ExplicitBounds: []float64{0.1, 0.5, 1}, BucketCounts: []uint64{1, 2, 2, 1},
					Attributes: []*commonpb.KeyValue{kv("http.route", str("/cart"))},
				},
				{TimeUnixNano: nanos(t0), Count: 2},                                                // no buckets
				{TimeUnixNano: nanos(t0), ExplicitBounds: []float64{1}, BucketCounts: []uint64{1}}, // mismatch
			},
		}}},
		{Name: "rpc.size", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
			DataPoints: []*metricspb.ExponentialHistogramDataPoint{{TimeUnixNano: nanos(t0)}},
		}}},
		{Name: "rpc.latency", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{
			DataPoints: []*metricspb.SummaryDataPoint{{TimeUnixNano: nanos(t0)}},
		}}},
		sum("queue.delta", metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA, false, intPoint(t0, 1)),
		sum("no.temporality", metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED, true, intPoint(t0, 1)),
		sum("", cumulative, true, intPoint(t0, 1)),
	}
}

func pointsByName(points []telemetry.MetricPoint) map[string][]telemetry.MetricPoint {
	out := map[string][]telemetry.MetricPoint{}
	for _, p := range points {
		out[p.Name] = append(out[p.Name], p)
	}
	return out
}

func TestDecodeAndConvertMetrics(t *testing.T) {
	scope := &commonpb.InstrumentationScope{Name: "app"}
	req := &metricspb.MetricsData{ResourceMetrics: []*metricspb.ResourceMetrics{
		resourceMetrics("a", scope, metricsFixture()...),
		resourceMetrics("b", scope, metricsFixture()...),
	}}
	body, err := proto.Marshal(req)
	require.NoError(t, err)
	decoded, err := otlp.DecodeMetrics(body)
	require.NoError(t, err)
	c := otlp.ConvertMetrics(decoded, "p1")

	// Per resource: 2 orders, 1 gauge, 1 up-down, 2 histograms are kept;
	// no value, no time, mismatch, exponential, summary, delta up-down,
	// unspecified temporality, and no name are rejected.
	require.Len(t, c.Points, 12)
	assert.Equal(t, int64(16), c.Rejected)
	assert.Equal(t, "metric has no name", c.RejectReason)
	assert.Empty(t, c.Logs)
	assert.Empty(t, c.Spans)
	assert.Len(t, c.Batch().Points, 12)

	byName := pointsByName(c.Points)
	orders := byName["orders.created"]
	require.Len(t, orders, 4)
	o := orders[0]
	assert.Equal(t, t0.Truncate(time.Microsecond), o.TS)
	assert.Equal(t, t0.Add(-time.Hour).Truncate(time.Microsecond), o.StartTS)
	assert.Equal(t, "p1", o.Project)
	assert.Equal(t, "api", o.Service)
	assert.Equal(t, "prod", o.Environment)
	assert.Equal(t, "1.4.0", o.Version)
	assert.Equal(t, int16(telemetry.TypeSum), o.Type)
	assert.Equal(t, int16(telemetry.TemporalityCumulative), o.Temporality)
	assert.True(t, o.Monotonic)
	require.NotNil(t, o.ValueInt)
	assert.Equal(t, int64(7), *o.ValueInt)
	assert.Nil(t, o.ValueDouble)
	assert.JSONEq(t, `{"plan":"pro"}`, o.Attributes)
	assert.JSONEq(t, `{"service.name":"api","service.version":"1.4.0","deployment.environment.name":"prod","service.instance.id":"a"}`, o.Resource)

	gauge := byName["queue.depth"]
	require.Len(t, gauge, 2)
	assert.Equal(t, int16(telemetry.TypeGauge), gauge[0].Type)
	assert.Equal(t, int16(0), gauge[0].Temporality)
	require.NotNil(t, gauge[0].ValueDouble)
	assert.InDelta(t, 4.5, *gauge[0].ValueDouble, 0)
	assert.True(t, gauge[0].StartTS.IsZero())
	assert.Empty(t, gauge[0].Attributes)

	updown := byName["connections.active"][0]
	assert.False(t, updown.Monotonic)
	assert.Equal(t, int16(telemetry.TypeSum), updown.Type)

	hist := byName["http.server.request.duration"]
	require.Len(t, hist, 4)
	h := hist[0]
	assert.Equal(t, int16(telemetry.TypeHistogram), h.Type)
	assert.Equal(t, "s", h.Unit)
	assert.Equal(t, uint64(6), *h.Count)
	assert.InDelta(t, 2.75, *h.Sum, 0)
	assert.InDelta(t, 0.05, *h.Min, 0)
	assert.InDelta(t, 1.5, *h.Max, 0)
	assert.Equal(t, []uint64{1, 2, 2, 1}, h.BucketCounts)
	assert.Equal(t, []float64{0.1, 0.5, 1}, h.BucketBounds)
	noBuckets := hist[1]
	assert.Equal(t, uint64(2), *noBuckets.Count)
	assert.Nil(t, noBuckets.Sum)
	assert.Nil(t, noBuckets.BucketCounts)
	assert.Nil(t, noBuckets.BucketBounds)

	// The same metric and attributes from two replicas are two series.
	assert.NotEqual(t, orders[0].SeriesHash, orders[2].SeriesHash)
	assert.NotEqual(t, orders[0].SeriesHash, orders[1].SeriesHash)
}

func hashOf(t *testing.T, project string, scope *commonpb.InstrumentationScope, attrs ...*commonpb.KeyValue) uint64 {
	t.Helper()
	req := &metricspb.MetricsData{ResourceMetrics: []*metricspb.ResourceMetrics{
		resourceMetrics("a", scope, sum("orders.created", cumulative, true, intPoint(t0, 1, attrs...))),
	}}
	c := otlp.ConvertMetrics(req, project)
	require.Len(t, c.Points, 1)
	return c.Points[0].SeriesHash
}

func TestSeriesHash(t *testing.T) {
	scope := &commonpb.InstrumentationScope{Name: "app", Version: "1"}
	base := hashOf(t, "p1", scope, kv("a", str("1")), kv("b", two))

	assert.Equal(t, base, hashOf(t, "p1", scope, kv("a", str("1")), kv("b", two)), "stable")
	assert.Equal(t, base, hashOf(t, "p1", scope, kv("b", two), kv("a", str("1"))), "attribute order")
	assert.NotEqual(t, base, hashOf(t, "p2", scope, kv("a", str("1")), kv("b", two)), "project")
	assert.NotEqual(t, base, hashOf(t, "p1", &commonpb.InstrumentationScope{Name: "app", Version: "2"},
		kv("a", str("1")), kv("b", two)), "scope version")
	assert.NotEqual(t, base, hashOf(t, "p1", scope, kv("a", str("1"))), "attributes")
}

func TestConvertMetricsUnknownService(t *testing.T) {
	req := &metricspb.MetricsData{ResourceMetrics: []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			sum("jobs", cumulative, true, intPoint(t0, 1)),
		}}},
	}}}
	c := otlp.ConvertMetrics(req, "p1")
	require.Len(t, c.Points, 1)
	assert.Equal(t, otlp.UnknownService, c.Points[0].Service)
	assert.Equal(t, otlp.DefaultEnvironment, c.Points[0].Environment)
	assert.Equal(t, int64(1), c.UnknownService)
}

func TestConvertMetricsTimeOutOfRange(t *testing.T) {
	req := &metricspb.MetricsData{ResourceMetrics: []*metricspb.ResourceMetrics{
		resourceMetrics("a", nil, sum("jobs", cumulative, true, &metricspb.NumberDataPoint{
			TimeUnixNano: 1 << 63, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1},
		})),
	}}
	c := otlp.ConvertMetrics(req, "p1")
	assert.Empty(t, c.Points)
	assert.Equal(t, "timestamp out of range", c.RejectReason)
}

func TestDecodeMetricsMalformed(t *testing.T) {
	_, err := otlp.DecodeMetrics([]byte{0xff, 0xff, 0xff})
	require.Error(t, err)
}
