package app_test

import (
	"bytes"
	"context"
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
		if !stopped {
			cancel()
			<-runErr
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

	cancel()
	select {
	case err := <-runErr:
		stopped = true
		require.NoError(t, err)
	case <-time.After(cfg.ShutdownTimeout):
		t.Fatal("Run did not return within the shutdown timeout")
	}
}
