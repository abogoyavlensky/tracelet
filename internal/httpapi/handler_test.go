package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/ingest"
)

func TestHealthReportsVersion(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{Info: httpapi.Info{Version: "1.2.3"}})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body.Status)
	assert.Equal(t, "1.2.3", body.Version)
}

func TestUnknownRouteIsJSONNotFound(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"error":"not found"}`, rec.Body.String())
}

type fakeIngestStats struct{ st ingest.Stats }

func (f fakeIngestStats) Stats() ingest.Stats { return f.st }

type fakeStorageStats struct {
	rep httpapi.StorageReport
	err error
}

func (f fakeStorageStats) StorageStats(context.Context) (httpapi.StorageReport, error) {
	return f.rep, f.err
}

func TestHealthReportsCounters(t *testing.T) {
	last := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	h := httpapi.NewHandler(httpapi.Deps{
		Info:        httpapi.Info{Version: "1.2.3"},
		IngestStats: fakeIngestStats{st: ingest.Stats{AcceptedRows: 250, Commits: 2, QueueDepth: 1, LastCommit: last}},
		StorageStats: fakeStorageStats{rep: httpapi.StorageReport{
			HotHours: map[string]int{"logs": 2}, ColdFiles: 3, ColdBytes: 4096, DiskFree: 1 << 30,
			LastFlush: last, LastRetention: &httpapi.RetentionReport{At: last, FilesDeleted: 1},
		}},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "1.2.3", body["version"])
	assert.Equal(t, true, body["ready"])
	ing := body["ingest"].(map[string]any)
	assert.InDelta(t, 250, ing["accepted_rows"], 0)
	assert.InDelta(t, 2, ing["commits"], 0)
	assert.InDelta(t, 1, ing["queue_depth"], 0)
	storage := body["storage"].(map[string]any)
	assert.Equal(t, map[string]any{"logs": float64(2)}, storage["hot_hours"])
	assert.InDelta(t, 3, storage["cold_files"], 0)
	assert.InDelta(t, 1, storage["last_retention"].(map[string]any)["files_deleted"], 0)
}

func TestHealthDegradedWhenStorageStatsFail(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{
		StorageStats: fakeStorageStats{err: assert.AnError},
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"degraded"`)
	assert.NotContains(t, rec.Body.String(), assert.AnError.Error())
}
