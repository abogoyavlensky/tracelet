package otlp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// DecodeTraces parses an OTLP/HTTP protobuf traces request body.
//
// ExportTraceServiceRequest is field 1, repeated ResourceSpans, exactly the
// wire layout of TracesData, so the body decodes into TracesData, as for logs.
func DecodeTraces(body []byte) (*tracepb.TracesData, error) {
	var req tracepb.TracesData
	if err := proto.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode traces request: %w", err)
	}
	return &req, nil
}

// ConvertSpans turns a request into span rows of the given project. A span
// whose trace or span ID has the wrong length, that has no start time, or
// that has any timestamp past what int64 nanoseconds hold, is rejected; the
// rest of the request is kept. trace_state, flags, and the dropped counts are
// not stored.
func ConvertSpans(req *tracepb.TracesData, projectID string) Converted {
	var c Converted
	for _, rs := range req.GetResourceSpans() {
		service, environment, version, resource, unknown := resourceInfo(rs.GetResource().GetAttributes())
		for _, ss := range rs.GetScopeSpans() {
			scope := ss.GetScope().GetName()
			for _, sp := range ss.GetSpans() {
				if reason := checkSpan(sp); reason != "" {
					c.Rejected++
					c.RejectReason = reason
					continue
				}
				if unknown {
					c.UnknownService++
				}
				ts := unixNano(sp.GetStartTimeUnixNano())
				end := ts
				if sp.GetEndTimeUnixNano() != 0 {
					end = unixNano(sp.GetEndTimeUnixNano())
				}
				c.Spans = append(c.Spans, telemetry.Span{
					TS:            ts,
					EndTS:         end,
					Project:       projectID,
					Service:       service,
					Environment:   environment,
					Version:       version,
					TraceID:       hex.EncodeToString(sp.GetTraceId()),
					SpanID:        hex.EncodeToString(sp.GetSpanId()),
					ParentSpanID:  hex.EncodeToString(sp.GetParentSpanId()),
					Name:          sp.GetName(),
					Kind:          int16(sp.GetKind()),
					StatusCode:    int16(sp.GetStatus().GetCode()),
					StatusMessage: sp.GetStatus().GetMessage(),
					DurationNS:    max(end.Sub(ts).Nanoseconds(), 0),
					Scope:         scope,
					Resource:      resource,
					Attributes:    jsonObject(sp.GetAttributes()),
					Events:        events(sp.GetEvents()),
					Links:         links(sp.GetLinks()),
				})
			}
		}
	}
	return c
}

// checkSpan returns why a span is rejected, or "".
func checkSpan(sp *tracepb.Span) string {
	switch {
	case len(sp.GetTraceId()) != 16:
		return "span trace_id must be 16 bytes"
	case len(sp.GetSpanId()) != 8:
		return "span span_id must be 8 bytes"
	case sp.GetStartTimeUnixNano() == 0:
		return "span has no start_time_unix_nano"
	}
	if sp.GetStartTimeUnixNano() > math.MaxInt64 || sp.GetEndTimeUnixNano() > math.MaxInt64 {
		return "timestamp out of range"
	}
	for _, e := range sp.GetEvents() {
		if e.GetTimeUnixNano() > math.MaxInt64 {
			return "timestamp out of range"
		}
	}
	return ""
}

func unixNano(n uint64) time.Time { return time.Unix(0, int64(n)).UTC() }

type event struct {
	TS         string         `json:"ts,omitempty"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type link struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// events encodes span events as a JSON array, or "" (stored as NULL) when
// there are none. An event without a time has no ts.
func events(es []*tracepb.Span_Event) string {
	if len(es) == 0 {
		return ""
	}
	out := make([]event, len(es))
	for i, e := range es {
		out[i] = event{Name: e.GetName(), Attributes: attributes(e.GetAttributes())}
		if e.GetTimeUnixNano() != 0 {
			out[i].TS = unixNano(e.GetTimeUnixNano()).Format(time.RFC3339Nano)
		}
	}
	return jsonArray(out)
}

// links encodes span links as a JSON array, or "" when there are none.
func links(ls []*tracepb.Span_Link) string {
	if len(ls) == 0 {
		return ""
	}
	out := make([]link, len(ls))
	for i, l := range ls {
		out[i] = link{
			TraceID:    hex.EncodeToString(l.GetTraceId()),
			SpanID:     hex.EncodeToString(l.GetSpanId()),
			Attributes: attributes(l.GetAttributes()),
		}
	}
	return jsonArray(out)
}

func attributes(kvs []*commonpb.KeyValue) map[string]any {
	if len(kvs) == 0 {
		return nil
	}
	return object(kvs)
}

func jsonArray(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// value never produces anything encoding/json rejects.
		panic(fmt.Sprintf("encode span array: %v", err))
	}
	return string(b)
}
