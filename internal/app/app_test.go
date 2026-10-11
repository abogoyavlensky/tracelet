package app_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/app"
	"github.com/abogoyavlensky/tracelet/internal/otlp"
)

const adminToken = "tl_e2eadmintoken0123456789abcdef"

// server is a running app and a client for it.
type server struct {
	t    *testing.T
	base string
}

func (s server) call(method, path, token, contentType string, body []byte) (int, []byte) {
	s.t.Helper()
	req, err := http.NewRequestWithContext(s.t.Context(), method, s.base+path, bytes.NewReader(body))
	require.NoError(s.t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(s.t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(s.t, err)
	return resp.StatusCode, data
}

func (s server) postJSON(path, token string, v any) map[string]any {
	s.t.Helper()
	body, err := json.Marshal(v)
	require.NoError(s.t, err)
	code, data := s.call(http.MethodPost, path, token, "application/json", body)
	require.Equal(s.t, http.StatusCreated, code, string(data))
	var out map[string]any
	require.NoError(s.t, json.Unmarshal(data, &out))
	return out
}

func (s server) getJSON(path, token string, out any) {
	s.t.Helper()
	code, data := s.call(http.MethodGet, path, token, "", nil)
	require.Equal(s.t, http.StatusOK, code, string(data))
	require.NoError(s.t, json.Unmarshal(data, out))
}

func str(v string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}
}

// exportRequest builds 250 records over two services and three severities,
// all inside the last ten minutes.
func exportRequest(t *testing.T, now time.Time) ([]byte, map[logspb.SeverityNumber]int) {
	t.Helper()
	severities := []logspb.SeverityNumber{
		logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
		logspb.SeverityNumber_SEVERITY_NUMBER_WARN,
		logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
	}
	counts := map[logspb.SeverityNumber]int{}
	var resources []*logspb.ResourceLogs
	for s, service := range []string{"api", "worker"} {
		var records []*logspb.LogRecord
		for i := range 125 {
			sev := severities[(s*125+i)%len(severities)]
			counts[sev]++
			records = append(records, &logspb.LogRecord{
				TimeUnixNano:   uint64(now.Add(-time.Duration(s*125+i) * time.Second).UnixNano()),
				SeverityNumber: sev,
				SeverityText:   sev.String(),
				Body:           str(fmt.Sprintf("%s record %d", service, i)),
			})
		}
		resources = append(resources, &logspb.ResourceLogs{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: str(service)},
				{Key: "deployment.environment.name", Value: str("test")},
			}},
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}},
		})
	}
	body, err := proto.Marshal(&logspb.LogsData{ResourceLogs: resources})
	require.NoError(t, err)
	return body, counts
}

// traceExport is one trace across two services: api serves GET /checkout
// and calls POST /charge, which worker consumes and fails. It also returns a
// log record from worker inside that span.
type traceExport struct {
	traces, logs []byte
	traceID      string
	start, end   time.Time
}

func exportTrace(t *testing.T, start time.Time) traceExport {
	t.Helper()
	traceID := []byte{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
	rootID := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	clientID := []byte{0, 0, 0, 0, 0, 0, 0, 2}
	workerID := []byte{0, 0, 0, 0, 0, 0, 0, 3}
	at := func(d time.Duration) uint64 { return uint64(start.Add(d).UnixNano()) }
	resource := func(service string) *resourcepb.Resource {
		return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: str(service)}}}
	}

	traces, err := proto.Marshal(&tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{
		{Resource: resource("api"), ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{
			{
				TraceId: traceID, SpanId: rootID, Name: "GET /checkout", Kind: tracepb.Span_SPAN_KIND_SERVER,
				StartTimeUnixNano: at(0), EndTimeUnixNano: at(200 * time.Millisecond),
			},
			{
				TraceId: traceID, SpanId: clientID, ParentSpanId: rootID, Name: "POST /charge", Kind: tracepb.Span_SPAN_KIND_CLIENT,
				StartTimeUnixNano: at(10 * time.Millisecond), EndTimeUnixNano: at(160 * time.Millisecond),
			},
		}}}},
		{Resource: resource("worker"), ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: traceID, SpanId: workerID, ParentSpanId: clientID, Name: "charge", Kind: tracepb.Span_SPAN_KIND_CONSUMER,
			StartTimeUnixNano: at(30 * time.Millisecond), EndTimeUnixNano: at(130 * time.Millisecond),
			Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "card declined"},
		}}}}},
	}})
	require.NoError(t, err)

	logs, err := proto.Marshal(&logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: resource("worker"),
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: at(50 * time.Millisecond), SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
			Body: str("card declined"), TraceId: traceID, SpanId: workerID,
		}}}},
	}}})
	require.NoError(t, err)

	return traceExport{
		traces: traces, logs: logs, traceID: hex.EncodeToString(traceID),
		start: start, end: start.Add(200 * time.Millisecond),
	}
}

func TestEndToEnd(t *testing.T) {
	cfg := app.Config{
		Addr:                "127.0.0.1:0",
		DataDir:             t.TempDir(),
		Version:             "e2e",
		ShutdownTimeout:     10 * time.Second,
		DuckDBMemoryLimit:   "256MB",
		DuckDBThreads:       2,
		RetentionDays:       30,
		DiskFloorBytes:      0,
		MaintenanceInterval: time.Minute,
		AdminToken:          adminToken,
	}
	a, err := app.New(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	assert.Empty(t, a.BootstrapToken(), "a seeded admin token is not printed")

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- a.Run(ctx) }()
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		cancel()
		select {
		case <-runErr:
		case <-time.After(cfg.ShutdownTimeout):
			t.Error("Run did not return within the shutdown timeout")
		}
	})
	s := server{t: t, base: "http://" + a.Addr()}

	// Admin sets up a project and its tokens.
	s.postJSON("/api/v1/projects", adminToken, map[string]string{"slug": "shop", "name": "Shop"})
	ingestTok := s.postJSON("/api/v1/tokens", adminToken, map[string]string{"project": "shop", "scope": "ingest", "name": "api"})["token"].(string)
	readTok := s.postJSON("/api/v1/tokens", adminToken, map[string]string{"project": "shop", "scope": "read", "name": "cli"})["token"].(string)

	// An SDK exports one batch.
	body, counts := exportRequest(t, time.Now())
	code, resp := s.call(http.MethodPost, "/v1/logs", ingestTok, "application/x-protobuf", body)
	require.Equal(t, http.StatusOK, code, string(resp))
	rejected, _, err := otlp.ParsePartialSuccess(resp)
	require.NoError(t, err)
	assert.Zero(t, rejected, "no partial success")

	type page struct {
		Items []struct {
			Body    string `json:"body"`
			Service string `json:"service"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}

	// Errors only.
	var errorLogs page
	s.getJSON("/api/v1/logs?"+url.Values{"level": {"error"}, "limit": {"1000"}}.Encode(), readTok, &errorLogs)
	assert.Len(t, errorLogs.Items, counts[logspb.SeverityNumber_SEVERITY_NUMBER_ERROR])

	// Every record, in pages of 100.
	seen := map[string]bool{}
	pages := 0
	q := url.Values{"limit": {"100"}, "last": {"1h"}}
	for {
		var p page
		s.getJSON("/api/v1/logs?"+q.Encode(), readTok, &p)
		pages++
		for _, it := range p.Items {
			assert.False(t, seen[it.Body], "%q seen twice", it.Body)
			seen[it.Body] = true
		}
		if p.NextCursor == "" {
			break
		}
		q = url.Values{"limit": {"100"}, "cursor": {p.NextCursor}}
	}
	assert.Equal(t, 3, pages)
	assert.Len(t, seen, 250)

	var services struct {
		Items []struct {
			Service string `json:"service"`
			Logs    int    `json:"logs"`
		} `json:"items"`
	}
	s.getJSON("/api/v1/services", readTok, &services)
	require.Len(t, services.Items, 2)
	assert.ElementsMatch(t, []string{"api", "worker"}, []string{services.Items[0].Service, services.Items[1].Service})
	assert.Equal(t, 125, services.Items[0].Logs)

	var health struct {
		Status string `json:"status"`
		Ingest struct {
			AcceptedRows int `json:"accepted_rows"`
			Commits      int `json:"commits"`
		} `json:"ingest"`
	}
	s.getJSON("/api/v1/health", "", &health)
	assert.Equal(t, "ok", health.Status)
	assert.Equal(t, 250, health.Ingest.AcceptedRows)
	assert.GreaterOrEqual(t, health.Ingest.Commits, 1)

	// A read token cannot ingest.
	code, _ = s.call(http.MethodPost, "/v1/logs", readTok, "application/x-protobuf", body)
	assert.Equal(t, http.StatusForbidden, code)

	// An SDK exports a trace, loses the response, and exports it again; a
	// log record in the trace arrives through /v1/logs.
	tr := exportTrace(t, time.Now().Add(-30*time.Second))
	for range 2 {
		code, resp = s.call(http.MethodPost, "/v1/traces", ingestTok, "application/x-protobuf", tr.traces)
		require.Equal(t, http.StatusOK, code, string(resp))
		rejected, _, err = otlp.ParsePartialSuccess(resp)
		require.NoError(t, err)
		assert.Zero(t, rejected)
	}
	code, resp = s.call(http.MethodPost, "/v1/logs", ingestTok, "application/x-protobuf", tr.logs)
	require.Equal(t, http.StatusOK, code, string(resp))

	var trace struct {
		TraceID    string    `json:"trace_id"`
		Start      time.Time `json:"start"`
		End        time.Time `json:"end"`
		DurationNS int64     `json:"duration_ns"`
		Services   []string  `json:"services"`
		Spans      []struct {
			Name         string `json:"name"`
			ParentSpanID string `json:"parent_span_id"`
		} `json:"spans"`
	}
	s.getJSON("/api/v1/traces/"+tr.traceID, readTok, &trace)
	require.Len(t, trace.Spans, 3, "the retry stored nothing twice")
	assert.Equal(t, []string{"api", "worker"}, trace.Services)
	assert.Equal(t, int64(200*time.Millisecond), trace.DurationNS)
	assert.Equal(t, "GET /checkout", trace.Spans[0].Name)

	// The trace's range finds its logs; to is exclusive.
	var traceLogs page
	s.getJSON("/api/v1/logs?"+url.Values{
		"trace_id": {tr.traceID},
		"from":     {trace.Start.Format(time.RFC3339Nano)},
		"to":       {trace.End.Add(time.Second).Format(time.RFC3339Nano)},
	}.Encode(), readTok, &traceLogs)
	require.Len(t, traceLogs.Items, 1)
	assert.Equal(t, "card declined", traceLogs.Items[0].Body)

	var spans struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	s.getJSON("/api/v1/spans?root=true", readTok, &spans)
	require.Len(t, spans.Items, 1)
	assert.Equal(t, "GET /checkout", spans.Items[0].Name)
	s.getJSON("/api/v1/spans?"+url.Values{"service": {"worker"}, "status": {"error"}}.Encode(), readTok, &spans)
	require.Len(t, spans.Items, 1)
	assert.Equal(t, "charge", spans.Items[0].Name)

	var withSpans struct {
		Items []struct {
			Service string `json:"service"`
			Logs    int    `json:"logs"`
			Spans   int    `json:"spans"`
		} `json:"items"`
	}
	s.getJSON("/api/v1/services", readTok, &withSpans)
	require.Len(t, withSpans.Items, 2)
	byService := map[string][2]int{}
	for _, it := range withSpans.Items {
		byService[it.Service] = [2]int{it.Logs, it.Spans}
	}
	assert.Equal(t, map[string][2]int{"api": {125, 2}, "worker": {126, 1}}, byService)

	var healthAfter struct {
		Ingest struct {
			AcceptedRows   int `json:"accepted_rows"`
			DuplicateSpans int `json:"duplicate_spans"`
		} `json:"ingest"`
	}
	s.getJSON("/api/v1/health", "", &healthAfter)
	assert.Equal(t, 254, healthAfter.Ingest.AcceptedRows, "250 logs, the trace's log, and 3 spans")
	assert.Equal(t, 3, healthAfter.Ingest.DuplicateSpans)

	code, _ = s.call(http.MethodPost, "/v1/traces", readTok, "application/x-protobuf", tr.traces)
	assert.Equal(t, http.StatusForbidden, code, "a read token cannot ingest traces")
	code, resp = s.call(http.MethodGet, "/api/v1/traces/ffffffffffffffffffffffffffffffff", readTok, "", nil)
	assert.Equal(t, http.StatusNotFound, code, string(resp))

	cancel()
	select {
	case err := <-runErr:
		stopped = true
		require.NoError(t, err)
	case <-time.After(cfg.ShutdownTimeout):
		stopped = true // don't wait a second time in cleanup
		t.Fatal("Run did not return within the shutdown timeout")
	}
}
