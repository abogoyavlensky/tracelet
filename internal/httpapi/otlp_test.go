package httpapi_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/ingest"
	"github.com/abogoyavlensky/tracelet/internal/otlp"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// fakeIngester records submitted batches and returns err.
type fakeIngester struct {
	mu       sync.Mutex
	batches  []telemetry.Batch
	rejected int64
	err      error
}

func (f *fakeIngester) Submit(_ context.Context, b telemetry.Batch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.batches = append(f.batches, b)
	return nil
}

func (f *fakeIngester) RecordRejected(rejected, _ int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected += rejected
}

func newOTLPHandler(t *testing.T, ing *fakeIngester, pressure bool) http.Handler {
	t.Helper()
	return httpapi.NewHandler(httpapi.Deps{
		Auth: newFakeAuth(), Ingester: ing, Logger: discardLogger(),
		DiskPressure: func() bool { return pressure },
	})
}

func logsBody(t *testing.T, records ...*logspb.LogRecord) []byte {
	t.Helper()
	b, err := proto.Marshal(&logspb.LogsData{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{
			Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "api"}},
		}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}},
	}}})
	require.NoError(t, err)
	return b
}

func record() *logspb.LogRecord {
	return &logspb.LogRecord{TimeUnixNano: uint64(time.Now().UnixNano()), SeverityText: "INFO"}
}

func postLogs(h http.Handler, token, contentType, encoding string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	return serve(h, req)
}

func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	require.NoError(t, err)
	_, err = zw.Write(b)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestOTLPLogsAccepted(t *testing.T) {
	ing := &fakeIngester{}
	h := newOTLPHandler(t, ing, false)

	rec := postLogs(h, ingestToken, "application/x-protobuf", "", logsBody(t, record(), record()))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/x-protobuf", rec.Header().Get("Content-Type"))
	rejected, _, err := otlp.ParsePartialSuccess(rec.Body.Bytes())
	require.NoError(t, err)
	assert.Zero(t, rejected)

	require.Len(t, ing.batches, 1)
	require.Len(t, ing.batches[0].Logs, 2)
	assert.Equal(t, "p1", ing.batches[0].Logs[0].Project, "rows carry the token's project ID")
	assert.Equal(t, "api", ing.batches[0].Logs[0].Service)
}

func TestOTLPLogsGzip(t *testing.T) {
	ing := &fakeIngester{}
	h := newOTLPHandler(t, ing, false)

	rec := postLogs(h, ingestToken, "application/x-protobuf", "gzip", gzipped(t, logsBody(t, record())))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, ing.batches, 1)
}

func TestOTLPLogsRejections(t *testing.T) {
	bomb := gzipped(t, make([]byte, 8<<20))
	// Deflate tops out near 1,000:1, so this is about 8 KB on the wire.
	require.Less(t, len(bomb), 10<<10, "8 MB of zeros compresses small")

	tests := []struct {
		name        string
		token       string
		contentType string
		encoding    string
		body        []byte
		want        int
		contains    string
	}{
		{"json", ingestToken, "application/json", "", []byte("{}"), http.StatusUnsupportedMediaType, "protobuf"},
		{"no content type", ingestToken, "", "", []byte{}, http.StatusUnsupportedMediaType, "x-protobuf"},
		{"raw body too large", ingestToken, "application/x-protobuf", "", make([]byte, httpapi.MaxOTLPBody+1), http.StatusRequestEntityTooLarge, "4 MB"},
		{"gzip bomb", ingestToken, "application/x-protobuf", "gzip", bomb, http.StatusRequestEntityTooLarge, "decompressed"},
		{"bad gzip", ingestToken, "application/x-protobuf", "gzip", []byte("nope"), http.StatusBadRequest, "gzip"},
		{"unknown encoding", ingestToken, "application/x-protobuf", "br", []byte{}, http.StatusUnsupportedMediaType, "gzip"},
		{"malformed", ingestToken, "application/x-protobuf", "", []byte{0xff, 0xff, 0xff}, http.StatusBadRequest, "malformed"},
		{"read token", readToken, "application/x-protobuf", "", []byte{}, http.StatusForbidden, "ingest"},
		{"admin token has no project", adminToken, "application/x-protobuf", "", []byte{}, http.StatusForbidden, "project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ing := &fakeIngester{}
			rec := postLogs(newOTLPHandler(t, ing, false), tt.token, tt.contentType, tt.encoding, tt.body)
			assert.Equal(t, tt.want, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tt.contains)
			assert.Empty(t, ing.batches)
		})
	}
}

func TestOTLPLogsDiskPressure(t *testing.T) {
	ing := &fakeIngester{}
	rec := postLogs(newOTLPHandler(t, ing, true), ingestToken, "application/x-protobuf", "", logsBody(t, record()))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "30", rec.Header().Get("Retry-After"))
	assert.Contains(t, rec.Body.String(), "disk")
	assert.Empty(t, ing.batches)
}

func TestOTLPLogsOverloaded(t *testing.T) {
	ing := &fakeIngester{err: ingest.ErrOverloaded}
	rec := postLogs(newOTLPHandler(t, ing, false), ingestToken, "application/x-protobuf", "", logsBody(t, record()))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
	assert.NotEqual(t, "application/x-protobuf", rec.Header().Get("Content-Type"), "no partial success")
}

func TestOTLPLogsWriterError(t *testing.T) {
	ing := &fakeIngester{err: assert.AnError}
	rec := postLogs(newOTLPHandler(t, ing, false), ingestToken, "application/x-protobuf", "", logsBody(t, record()))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), assert.AnError.Error())
}

func TestOTLPLogsPartialSuccess(t *testing.T) {
	ing := &fakeIngester{}
	rec := postLogs(newOTLPHandler(t, ing, false), ingestToken, "application/x-protobuf", "",
		logsBody(t, record(), &logspb.LogRecord{}))
	require.Equal(t, http.StatusOK, rec.Code)

	rejected, msg, err := otlp.ParsePartialSuccess(rec.Body.Bytes())
	require.NoError(t, err)
	assert.Equal(t, int64(1), rejected)
	assert.NotEmpty(t, msg)
	assert.Equal(t, int64(1), ing.rejected)
	require.Len(t, ing.batches, 1)
	assert.Len(t, ing.batches[0].Logs, 1)
}
