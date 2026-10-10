// Package otlp decodes OTLP requests and converts them into telemetry rows.
package otlp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// UnknownService is the service of records whose resource has no
// service.name, as the OpenTelemetry SDKs default it.
const UnknownService = "unknown_service"

// DefaultEnvironment is the environment of records whose resource has no
// deployment.environment.name.
const DefaultEnvironment = "default"

// Resource attributes Tracelet files rows under.
const (
	attrServiceName    = "service.name"
	attrServiceVersion = "service.version"
	attrEnvironment    = "deployment.environment.name"
)

// Decode parses an OTLP/HTTP protobuf logs request body.
//
// ExportLogsServiceRequest is field 1, repeated ResourceLogs, exactly the
// wire layout of LogsData, so the body decodes into LogsData. That keeps the
// collector package, which imports gRPC, out of the build.
func Decode(body []byte) (*logspb.LogsData, error) {
	var req logspb.LogsData
	if err := proto.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode logs request: %w", err)
	}
	return &req, nil
}

// Converted is the result of converting one request: the rows to store, and
// what was rejected and why, for the partial success response.
type Converted struct {
	Logs           []telemetry.Log
	Rejected       int64
	RejectReason   string
	UnknownService int64
}

// ConvertLogs turns a request into rows of the given project. A record with
// neither a time nor an observed time, or a time past what int64 nanoseconds
// hold, is rejected; the rest of the request is kept.
func ConvertLogs(req *logspb.LogsData, projectID string) Converted {
	var c Converted
	for _, rl := range req.GetResourceLogs() {
		resAttrs := rl.GetResource().GetAttributes()
		service := stringAttr(resAttrs, attrServiceName)
		unknown := service == ""
		if unknown {
			service = UnknownService
		}
		resource := jsonObject(resAttrs)
		environment := stringAttr(resAttrs, attrEnvironment)
		if environment == "" {
			environment = DefaultEnvironment
		}
		version := stringAttr(resAttrs, attrServiceVersion)

		for _, sl := range rl.GetScopeLogs() {
			scope := sl.GetScope().GetName()
			for _, rec := range sl.GetLogRecords() {
				ts, observed, reason := timestamps(rec.GetTimeUnixNano(), rec.GetObservedTimeUnixNano())
				if reason != "" {
					c.Rejected++
					c.RejectReason = reason
					continue
				}
				if unknown {
					c.UnknownService++
				}
				c.Logs = append(c.Logs, telemetry.Log{
					TS:             ts,
					ObservedTS:     observed,
					Project:        projectID,
					Service:        service,
					Environment:    environment,
					Version:        version,
					SeverityNumber: int16(rec.GetSeverityNumber()),
					SeverityText:   rec.GetSeverityText(),
					Body:           body(rec.GetBody()),
					TraceID:        hex.EncodeToString(rec.GetTraceId()),
					SpanID:         hex.EncodeToString(rec.GetSpanId()),
					Scope:          scope,
					Resource:       resource,
					Attributes:     jsonObject(rec.GetAttributes()),
				})
			}
		}
	}
	return c
}

// timestamps picks the row's time: the record's time, or its observed time
// when the record has none. It returns a rejection reason when neither works.
func timestamps(timeNano, observedNano uint64) (ts, observed time.Time, reason string) {
	if timeNano > math.MaxInt64 || observedNano > math.MaxInt64 {
		return time.Time{}, time.Time{}, "timestamp out of range"
	}
	if observedNano != 0 {
		observed = time.Unix(0, int64(observedNano)).UTC()
	}
	switch {
	case timeNano != 0:
		ts = time.Unix(0, int64(timeNano)).UTC()
	case observedNano != 0:
		ts = observed
	default:
		return time.Time{}, time.Time{}, "log record has neither time_unix_nano nor observed_time_unix_nano"
	}
	return ts, observed, ""
}

// body is a string body as is, and any other value kind as its JSON.
func body(v *commonpb.AnyValue) string {
	if v == nil || v.GetValue() == nil {
		return ""
	}
	if s, ok := v.GetValue().(*commonpb.AnyValue_StringValue); ok {
		return s.StringValue
	}
	b, err := json.Marshal(value(v))
	if err != nil {
		return ""
	}
	return string(b)
}

// jsonObject encodes attributes as a JSON object, or "" (stored as NULL)
// when there are none.
func jsonObject(kvs []*commonpb.KeyValue) string {
	if len(kvs) == 0 {
		return ""
	}
	b, err := json.Marshal(object(kvs))
	if err != nil {
		// value never produces anything encoding/json rejects.
		panic(fmt.Sprintf("encode attributes: %v", err))
	}
	return string(b)
}

// ExportLogsServiceResponse field numbers.
const (
	fieldPartialSuccess     = 1 // ExportLogsServiceResponse.partial_success
	fieldRejectedLogRecords = 1 // ExportLogsPartialSuccess.rejected_log_records
	fieldErrorMessage       = 2 // ExportLogsPartialSuccess.error_message
)

// PartialSuccess encodes the ExportLogsServiceResponse for a converted
// request: empty when nothing was rejected, as OTLP asks, and otherwise a
// partial_success naming the rejected count and the reason.
func PartialSuccess(c Converted) []byte {
	if c.Rejected == 0 {
		return []byte{}
	}
	var ps []byte
	ps = protowire.AppendTag(ps, fieldRejectedLogRecords, protowire.VarintType)
	ps = protowire.AppendVarint(ps, uint64(c.Rejected))
	if c.RejectReason != "" {
		ps = protowire.AppendTag(ps, fieldErrorMessage, protowire.BytesType)
		ps = protowire.AppendString(ps, c.RejectReason)
	}
	var out []byte
	out = protowire.AppendTag(out, fieldPartialSuccess, protowire.BytesType)
	return protowire.AppendBytes(out, ps)
}

// ParsePartialSuccess reads an ExportLogsServiceResponse, for clients and
// tests: the rejected count and message, zero when there is no partial
// success.
func ParsePartialSuccess(b []byte) (rejected int64, message string, err error) {
	ps, err := field(b, fieldPartialSuccess, protowire.BytesType)
	if err != nil || ps == nil {
		return 0, "", err
	}
	for len(ps) > 0 {
		num, typ, n := protowire.ConsumeTag(ps)
		if n < 0 {
			return 0, "", protowire.ParseError(n)
		}
		ps = ps[n:]
		switch {
		case num == fieldRejectedLogRecords && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(ps)
			if n < 0 {
				return 0, "", protowire.ParseError(n)
			}
			rejected, ps = int64(v), ps[n:]
		case num == fieldErrorMessage && typ == protowire.BytesType:
			v, n := protowire.ConsumeString(ps)
			if n < 0 {
				return 0, "", protowire.ParseError(n)
			}
			message, ps = v, ps[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, ps)
			if n < 0 {
				return 0, "", protowire.ParseError(n)
			}
			ps = ps[n:]
		}
	}
	return rejected, message, nil
}

// field returns the last occurrence of a length-delimited field, or nil.
func field(b []byte, want protowire.Number, wantType protowire.Type) ([]byte, error) {
	var out []byte
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
		if num == want && typ == wantType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			out, b = v, b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
	}
	return out, nil
}
