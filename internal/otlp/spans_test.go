package otlp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/otlp"
)

var (
	traceID  = []byte{0x4B, 0xF9, 0x2F, 0x35, 0x77, 0xB3, 0x4D, 0xA6, 0xA3, 0xCE, 0x92, 0x9D, 0x0E, 0x0E, 0x47, 0x36}
	spanID   = []byte{0x00, 0xF0, 0x67, 0xAA, 0x0B, 0xA9, 0x02, 0xB7}
	parentID = []byte{0xAB, 0, 0, 0, 0, 0, 0, 1}
)

func traces(resAttrs []*commonpb.KeyValue, spans ...*tracepb.Span) *tracepb.TracesData {
	return &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: resAttrs},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "http"},
			Spans: spans,
		}},
	}}}
}

func nanos(t time.Time) uint64 { return uint64(t.UnixNano()) }

func TestDecodeAndConvertSpans(t *testing.T) {
	end := t0.Add(120 * time.Millisecond)
	req := traces(
		[]*commonpb.KeyValue{
			kv("service.name", str("api")),
			kv("service.version", str("1.4.0")),
			kv("deployment.environment.name", str("prod")),
			kv("host.name", str("box")),
		},
		&tracepb.Span{
			TraceId:           traceID,
			SpanId:            spanID,
			ParentSpanId:      parentID,
			TraceState:        "k=v",
			Name:              "GET /cart",
			Kind:              tracepb.Span_SPAN_KIND_SERVER,
			StartTimeUnixNano: nanos(t0),
			EndTimeUnixNano:   nanos(end),
			Attributes: []*commonpb.KeyValue{
				kv("http.route", str("/cart")),
				kv("http.status_code", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 504}}),
				kv("retry", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}),
			},
			Events: []*tracepb.Span_Event{
				{TimeUnixNano: nanos(t0.Add(time.Millisecond)), Name: "exception", Attributes: []*commonpb.KeyValue{kv("exception.type", str("Timeout"))}},
				{TimeUnixNano: nanos(t0.Add(2 * time.Millisecond)), Name: "retry"},
			},
			Links:  []*tracepb.Span_Link{{TraceId: traceID, SpanId: parentID, Attributes: []*commonpb.KeyValue{kv("reason", str("batch"))}}},
			Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "deadline exceeded"},
		},
		&tracepb.Span{TraceId: traceID, SpanId: parentID, Name: "root", StartTimeUnixNano: nanos(t0)},
		&tracepb.Span{TraceId: traceID, SpanId: spanID, Name: "inverted", StartTimeUnixNano: nanos(t0), EndTimeUnixNano: nanos(t0.Add(-time.Second))},
		&tracepb.Span{TraceId: traceID, SpanId: spanID, Name: "no start"},
		&tracepb.Span{TraceId: traceID[:4], SpanId: spanID, Name: "short trace", StartTimeUnixNano: nanos(t0)},
		&tracepb.Span{TraceId: traceID, SpanId: spanID[:3], Name: "short span", StartTimeUnixNano: nanos(t0)},
	)
	body, err := proto.Marshal(req)
	require.NoError(t, err)

	decoded, err := otlp.DecodeTraces(body)
	require.NoError(t, err)
	c := otlp.ConvertSpans(decoded, "p1")

	require.Len(t, c.Spans, 3)
	assert.Empty(t, c.Logs)
	assert.Equal(t, int64(3), c.Rejected)
	assert.Equal(t, "span span_id must be 8 bytes", c.RejectReason)
	assert.Zero(t, c.UnknownService)

	s := c.Spans[0]
	assert.Equal(t, t0, s.TS)
	assert.Equal(t, end, s.EndTS)
	assert.Equal(t, "p1", s.Project)
	assert.Equal(t, "api", s.Service)
	assert.Equal(t, "prod", s.Environment)
	assert.Equal(t, "1.4.0", s.Version)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", s.TraceID)
	assert.Equal(t, "00f067aa0ba902b7", s.SpanID)
	assert.Equal(t, "ab00000000000001", s.ParentSpanID)
	assert.Equal(t, "GET /cart", s.Name)
	assert.Equal(t, int16(2), s.Kind)
	assert.Equal(t, int16(2), s.StatusCode)
	assert.Equal(t, "deadline exceeded", s.StatusMessage)
	assert.Equal(t, int64(120*time.Millisecond), s.DurationNS)
	assert.Equal(t, "http", s.Scope)
	assert.JSONEq(t, `{"service.name":"api","service.version":"1.4.0","deployment.environment.name":"prod","host.name":"box"}`, s.Resource)
	assert.JSONEq(t, `{"http.route":"/cart","http.status_code":504,"retry":true}`, s.Attributes)
	assert.JSONEq(t, `[
		{"ts":"2026-10-10T12:30:00.124456789Z","name":"exception","attributes":{"exception.type":"Timeout"}},
		{"ts":"2026-10-10T12:30:00.125456789Z","name":"retry"}
	]`, s.Events)
	assert.JSONEq(t, `[{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"ab00000000000001","attributes":{"reason":"batch"}}]`, s.Links)

	root := c.Spans[1]
	assert.Empty(t, root.ParentSpanID)
	assert.Equal(t, root.TS, root.EndTS, "no end time ends at the start")
	assert.Zero(t, root.DurationNS)
	assert.Empty(t, root.Events, "no events is NULL")
	assert.Empty(t, root.Links, "no links is NULL")
	assert.Empty(t, root.Attributes)

	inverted := c.Spans[2]
	assert.Zero(t, inverted.DurationNS)
	assert.Equal(t, t0.Add(-time.Second), inverted.EndTS, "the end is kept as sent")
}

func TestConvertSpansRejectReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		span   *tracepb.Span
		reason string
	}{
		{"short trace id", &tracepb.Span{TraceId: traceID[:4], SpanId: spanID, StartTimeUnixNano: nanos(t0)}, "span trace_id must be 16 bytes"},
		{"no span id", &tracepb.Span{TraceId: traceID, StartTimeUnixNano: nanos(t0)}, "span span_id must be 8 bytes"},
		{"zero trace id", &tracepb.Span{TraceId: make([]byte, 16), SpanId: spanID, StartTimeUnixNano: nanos(t0)}, "span trace_id must not be all zeros"},
		{"zero span id", &tracepb.Span{TraceId: traceID, SpanId: make([]byte, 8), StartTimeUnixNano: nanos(t0)}, "span span_id must not be all zeros"},
		{"no start", &tracepb.Span{TraceId: traceID, SpanId: spanID}, "span has no start_time_unix_nano"},
		{"start out of range", &tracepb.Span{TraceId: traceID, SpanId: spanID, StartTimeUnixNano: 1 << 63}, "timestamp out of range"},
		{"end out of range", &tracepb.Span{TraceId: traceID, SpanId: spanID, StartTimeUnixNano: nanos(t0), EndTimeUnixNano: 1 << 63}, "timestamp out of range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := otlp.ConvertSpans(traces(nil, tc.span), "p1")
			assert.Empty(t, c.Spans)
			assert.Equal(t, int64(1), c.Rejected)
			assert.Equal(t, tc.reason, c.RejectReason)
		})
	}
}

func TestConvertSpansUnknownService(t *testing.T) {
	c := otlp.ConvertSpans(traces(nil,
		&tracepb.Span{TraceId: traceID, SpanId: spanID, StartTimeUnixNano: nanos(t0)},
		&tracepb.Span{TraceId: traceID, SpanId: parentID, StartTimeUnixNano: nanos(t0)},
	), "p1")
	require.Len(t, c.Spans, 2)
	assert.Equal(t, otlp.UnknownService, c.Spans[0].Service)
	assert.Equal(t, int64(2), c.UnknownService)
	assert.Equal(t, otlp.DefaultEnvironment, c.Spans[0].Environment)
	assert.Empty(t, c.Spans[0].Resource, "no attributes is NULL, not {}")
}

func TestConvertedBatch(t *testing.T) {
	c := otlp.ConvertSpans(traces(nil, &tracepb.Span{TraceId: traceID, SpanId: spanID, StartTimeUnixNano: nanos(t0)}), "p1")
	b := c.Batch()
	assert.Len(t, b.Spans, 1)
	assert.Empty(t, b.Logs)
}

func TestDecodeTracesMalformed(t *testing.T) {
	_, err := otlp.DecodeTraces([]byte{0xff, 0xff, 0xff})
	require.Error(t, err)
}
