package otlp

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"time"

	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// DecodeMetrics parses an OTLP/HTTP protobuf metrics request body.
//
// ExportMetricsServiceRequest is field 1, repeated ResourceMetrics, exactly
// the wire layout of MetricsData, so the body decodes into MetricsData, as
// for logs and traces.
func DecodeMetrics(body []byte) (*metricspb.MetricsData, error) {
	var req metricspb.MetricsData
	if err := proto.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode metrics request: %w", err)
	}
	return &req, nil
}

// Rejection reasons for metric data points.
const (
	reasonNoName           = "metric has no name"
	reasonNoTime           = "data point has no time_unix_nano"
	reasonTimeRange        = "timestamp out of range"
	reasonNoValue          = "number data point has no value"
	reasonBuckets          = "histogram bucket_counts must have one more element than explicit_bounds"
	reasonSumTemporality   = "sum has no aggregation temporality"
	reasonHistTemporality  = "histogram has no aggregation temporality"
	reasonDeltaUpDown      = "delta up-down counters are not supported"
	reasonExponentialHisto = "exponential histogram metrics are not supported yet"
	reasonSummary          = "summary metrics are not supported"
)

// metricContext is what every point of one metric shares.
type metricContext struct {
	projectID, service, environment, version, resource string
	scopeName, scopeVersion, scopeAttrs                string
	unknown                                            bool
	name, unit                                         string
	typ, temporality                                   int16
	monotonic                                          bool
}

// ConvertMetrics turns a request into metric point rows of the given
// project. Gauges, sums, and explicit-bucket histograms are stored; every
// other kind of point, and every invalid one, is rejected with a reason and
// the rest of the request is kept. A point flagged NO_RECORDED_VALUE carries
// no data and is skipped without counting. Exemplars are not stored.
// Timestamps are truncated to microseconds, the hot table's resolution, so a
// retried point's key equals the stored row's.
func ConvertMetrics(req *metricspb.MetricsData, projectID string) Converted {
	var c Converted
	for _, rm := range req.GetResourceMetrics() {
		service, environment, version, resource, unknown := resourceInfo(rm.GetResource().GetAttributes())
		for _, sm := range rm.GetScopeMetrics() {
			mc := metricContext{
				projectID: projectID, service: service, environment: environment, version: version,
				resource: resource, unknown: unknown,
				scopeName:    sm.GetScope().GetName(),
				scopeVersion: sm.GetScope().GetVersion(),
				scopeAttrs:   jsonObject(sm.GetScope().GetAttributes()),
			}
			for _, m := range sm.GetMetrics() {
				convertMetric(&c, mc, m)
			}
		}
	}
	return c
}

func (c *Converted) reject(reason string, n int) {
	if n <= 0 {
		return
	}
	c.Rejected += int64(n)
	c.RejectReason = reason
}

func convertMetric(c *Converted, mc metricContext, m *metricspb.Metric) {
	mc.name, mc.unit = m.GetName(), m.GetUnit()
	switch data := m.GetData().(type) {
	case *metricspb.Metric_Gauge:
		if mc.name == "" {
			c.reject(reasonNoName, len(data.Gauge.GetDataPoints()))
			return
		}
		mc.typ = telemetry.TypeGauge
		for _, dp := range data.Gauge.GetDataPoints() {
			c.addNumber(mc, dp)
		}
	case *metricspb.Metric_Sum:
		points := data.Sum.GetDataPoints()
		mc.typ, mc.monotonic = telemetry.TypeSum, data.Sum.GetIsMonotonic()
		mc.temporality = int16(data.Sum.GetAggregationTemporality())
		switch {
		case mc.name == "":
			c.reject(reasonNoName, len(points))
			return
		case mc.temporality != telemetry.TemporalityDelta && mc.temporality != telemetry.TemporalityCumulative:
			c.reject(reasonSumTemporality, len(points))
			return
		case !mc.monotonic && mc.temporality == telemetry.TemporalityDelta:
			c.reject(reasonDeltaUpDown, len(points))
			return
		}
		for _, dp := range points {
			c.addNumber(mc, dp)
		}
	case *metricspb.Metric_Histogram:
		points := data.Histogram.GetDataPoints()
		mc.typ = telemetry.TypeHistogram
		mc.temporality = int16(data.Histogram.GetAggregationTemporality())
		switch {
		case mc.name == "":
			c.reject(reasonNoName, len(points))
			return
		case mc.temporality != telemetry.TemporalityDelta && mc.temporality != telemetry.TemporalityCumulative:
			c.reject(reasonHistTemporality, len(points))
			return
		}
		for _, dp := range points {
			c.addHistogram(mc, dp)
		}
	case *metricspb.Metric_ExponentialHistogram:
		c.reject(reasonExponentialHisto, len(data.ExponentialHistogram.GetDataPoints()))
	case *metricspb.Metric_Summary:
		c.reject(reasonSummary, len(data.Summary.GetDataPoints()))
	}
}

const noRecordedValue = uint32(metricspb.DataPointFlags_DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK)

// pointTimes reads a point's time and start time, or a rejection reason.
func pointTimes(timeNano, startNano uint64) (ts, start time.Time, reason string) {
	if timeNano > math.MaxInt64 || startNano > math.MaxInt64 {
		return time.Time{}, time.Time{}, reasonTimeRange
	}
	if timeNano == 0 {
		return time.Time{}, time.Time{}, reasonNoTime
	}
	ts = unixNano(timeNano).Truncate(time.Microsecond)
	if startNano != 0 {
		start = unixNano(startNano).Truncate(time.Microsecond)
	}
	return ts, start, ""
}

// point returns the row fields every point kind shares.
func (mc metricContext) point(ts, start time.Time, attrs string) telemetry.MetricPoint {
	return telemetry.MetricPoint{
		TS: ts, StartTS: start, Project: mc.projectID,
		Service: mc.service, Environment: mc.environment, Version: mc.version,
		Name: mc.name, Unit: mc.unit, Type: mc.typ, Temporality: mc.temporality, Monotonic: mc.monotonic,
		SeriesHash: seriesHash(mc.projectID, mc.name, mc.typ, mc.temporality, mc.monotonic, mc.unit,
			mc.scopeName, mc.scopeVersion, mc.scopeAttrs, mc.resource, attrs),
		Resource:   mc.resource,
		Attributes: attrs,
	}
}

func (c *Converted) addNumber(mc metricContext, dp *metricspb.NumberDataPoint) {
	if dp.GetFlags()&noRecordedValue != 0 {
		return
	}
	ts, start, reason := pointTimes(dp.GetTimeUnixNano(), dp.GetStartTimeUnixNano())
	if reason != "" {
		c.reject(reason, 1)
		return
	}
	p := mc.point(ts, start, jsonObject(dp.GetAttributes()))
	switch v := dp.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsInt:
		p.ValueInt = &v.AsInt
	case *metricspb.NumberDataPoint_AsDouble:
		p.ValueDouble = &v.AsDouble
	default:
		c.reject(reasonNoValue, 1)
		return
	}
	c.keep(mc, p)
}

func (c *Converted) addHistogram(mc metricContext, dp *metricspb.HistogramDataPoint) {
	if dp.GetFlags()&noRecordedValue != 0 {
		return
	}
	ts, start, reason := pointTimes(dp.GetTimeUnixNano(), dp.GetStartTimeUnixNano())
	if reason != "" {
		c.reject(reason, 1)
		return
	}
	counts, bounds := dp.GetBucketCounts(), dp.GetExplicitBounds()
	if len(counts) > 0 && len(counts) != len(bounds)+1 {
		c.reject(reasonBuckets, 1)
		return
	}
	p := mc.point(ts, start, jsonObject(dp.GetAttributes()))
	count := dp.GetCount()
	p.Count, p.Sum, p.Min, p.Max = &count, dp.Sum, dp.Min, dp.Max
	if len(counts) > 0 {
		p.BucketCounts, p.BucketBounds = counts, bounds
		if bounds == nil {
			p.BucketBounds = []float64{}
		}
	}
	c.keep(mc, p)
}

func (c *Converted) keep(mc metricContext, p telemetry.MetricPoint) {
	if mc.unknown {
		c.UnknownService++
	}
	c.Points = append(c.Points, p)
}

// seriesHash identifies a series: the whole context that emits it. Resource
// attributes are fixed for an SDK's lifetime, so replicas and restarted
// processes are separate series, and temporality, monotonicity, and unit are
// included so a configuration change never mixes points of different
// meaning. The JSON strings come from jsonObject, which sorts keys. Each
// field is length-prefixed.
func seriesHash(projectID, name string, typ, temporality int16, monotonic bool,
	unit, scopeName, scopeVersion, scopeAttrsJSON, resourceJSON, attrsJSON string,
) uint64 {
	h := fnv.New64a()
	mono := "0"
	if monotonic {
		mono = "1"
	}
	for _, s := range []string{
		projectID, name, strconv.Itoa(int(typ)), strconv.Itoa(int(temporality)), mono, unit,
		scopeName, scopeVersion, scopeAttrsJSON, resourceJSON, attrsJSON,
	} {
		// A length prefix keeps field boundaries unambiguous: OTLP strings
		// may contain NUL bytes, so no separator byte would.
		_, _ = h.Write(binary.AppendUvarint(nil, uint64(len(s))))
		_, _ = h.Write([]byte(s))
	}
	return h.Sum64()
}
