package otlp_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/otlp"
)

func str(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func kv(k string, v *commonpb.AnyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: v}
}

var t0 = time.Date(2026, 10, 10, 12, 30, 0, 123456789, time.UTC)

func request(resAttrs []*commonpb.KeyValue, records ...*logspb.LogRecord) *logspb.LogsData {
	return &logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: resAttrs},
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope:      &commonpb.InstrumentationScope{Name: "checkout"},
			LogRecords: records,
		}},
	}}}
}

func TestDecodeAndConvertFullRecord(t *testing.T) {
	req := request(
		[]*commonpb.KeyValue{
			kv("service.name", str("api")),
			kv("service.version", str("1.4.0")),
			kv("deployment.environment.name", str("prod")),
		},
		&logspb.LogRecord{
			TimeUnixNano:         uint64(t0.UnixNano()),
			ObservedTimeUnixNano: uint64(t0.Add(time.Second).UnixNano()),
			SeverityNumber:       logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
			SeverityText:         "ERROR",
			Body:                 str("payment failed"),
			Attributes:           []*commonpb.KeyValue{kv("exception.type", str("Timeout"))},
			TraceId:              []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
			SpanId:               []byte{0xAB, 0, 0, 0, 0, 0, 0, 1},
		},
	)
	body, err := proto.Marshal(req)
	require.NoError(t, err)

	decoded, err := otlp.Decode(body)
	require.NoError(t, err)
	c := otlp.ConvertLogs(decoded, "p1")

	require.Len(t, c.Logs, 1)
	assert.Zero(t, c.Rejected)
	l := c.Logs[0]
	assert.Equal(t, t0, l.TS)
	assert.Equal(t, t0.Add(time.Second), l.ObservedTS)
	assert.Equal(t, "p1", l.Project)
	assert.Equal(t, "api", l.Service)
	assert.Equal(t, "prod", l.Environment)
	assert.Equal(t, "1.4.0", l.Version)
	assert.Equal(t, int16(17), l.SeverityNumber)
	assert.Equal(t, "ERROR", l.SeverityText)
	assert.Equal(t, "payment failed", l.Body)
	assert.Equal(t, "0123456789abcdef0123456789abcdef", l.TraceID)
	assert.Equal(t, "ab00000000000001", l.SpanID)
	assert.Equal(t, "checkout", l.Scope)
	assert.JSONEq(t, `{"service.name":"api","service.version":"1.4.0","deployment.environment.name":"prod"}`, l.Resource)
	assert.JSONEq(t, `{"exception.type":"Timeout"}`, l.Attributes)
}

func TestConvertTimestamps(t *testing.T) {
	c := otlp.ConvertLogs(request(nil,
		&logspb.LogRecord{ObservedTimeUnixNano: uint64(t0.UnixNano())},
		&logspb.LogRecord{},
		&logspb.LogRecord{TimeUnixNano: math.MaxUint64},
	), "p1")

	require.Len(t, c.Logs, 1)
	assert.Equal(t, t0, c.Logs[0].TS, "observed time stands in")
	assert.Equal(t, int64(2), c.Rejected)
	assert.NotEmpty(t, c.RejectReason)
}

func TestConvertUnknownService(t *testing.T) {
	c := otlp.ConvertLogs(request(nil,
		&logspb.LogRecord{TimeUnixNano: uint64(t0.UnixNano())},
		&logspb.LogRecord{TimeUnixNano: uint64(t0.UnixNano())},
	), "p1")
	require.Len(t, c.Logs, 2)
	assert.Equal(t, otlp.UnknownService, c.Logs[0].Service)
	assert.Equal(t, int64(2), c.UnknownService)
	assert.Equal(t, otlp.DefaultEnvironment, c.Logs[0].Environment)
	assert.Empty(t, c.Logs[0].Resource, "no attributes is NULL, not {}")
	assert.Empty(t, c.Logs[0].Body)
}

func TestConvertAnyValues(t *testing.T) {
	nested := &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
		Values: []*commonpb.KeyValue{
			kv("s", str("x")),
			kv("i", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}),
			kv("d", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 1.5}}),
			kv("b", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}),
			kv("raw", &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: []byte("hi")}}),
			kv("nan", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: math.NaN()}}),
			kv("empty", &commonpb.AnyValue{}),
			kv("list", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{
				Values: []*commonpb.AnyValue{str("a"), {Value: &commonpb.AnyValue_IntValue{IntValue: 1}}},
			}}}),
		},
	}}}
	c := otlp.ConvertLogs(request(nil, &logspb.LogRecord{
		TimeUnixNano: uint64(t0.UnixNano()),
		Body:         nested,
		Attributes:   []*commonpb.KeyValue{kv("nested", nested)},
	}), "p1")
	require.Len(t, c.Logs, 1)

	want := `{"s":"x","i":42,"d":1.5,"b":true,"raw":"aGk=","nan":"NaN","empty":null,"list":["a",1]}`
	assert.JSONEq(t, want, c.Logs[0].Body, "a kvlist body is its JSON")
	var attrs map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(c.Logs[0].Attributes), &attrs))
	assert.JSONEq(t, want, string(attrs["nested"]))
}

func TestDecodeMalformed(t *testing.T) {
	_, err := otlp.Decode([]byte{0xff, 0xff, 0xff})
	require.Error(t, err)
}

func TestPartialSuccess(t *testing.T) {
	assert.Empty(t, otlp.PartialSuccess(otlp.Converted{}))

	b := otlp.PartialSuccess(otlp.Converted{Rejected: 1, RejectReason: "x"})
	// partial_success (1, bytes) { rejected_log_records (1, varint) = 1, error_message (2, bytes) = "x" }
	assert.Equal(t, []byte{0x0a, 0x05, 0x08, 0x01, 0x12, 0x01, 'x'}, b)

	rejected, msg, err := otlp.ParsePartialSuccess(b)
	require.NoError(t, err)
	assert.Equal(t, int64(1), rejected)
	assert.Equal(t, "x", msg)

	rejected, _, err = otlp.ParsePartialSuccess(nil)
	require.NoError(t, err)
	assert.Zero(t, rejected)
}
