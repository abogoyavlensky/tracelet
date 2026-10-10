package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/ingest"
)

// IngestStats reports the ingest counters.
type IngestStats interface {
	Stats() ingest.Stats
}

// StorageStats reports the state of hot and cold storage.
type StorageStats interface {
	StorageStats(ctx context.Context) (StorageReport, error)
}

// StorageReport is the storage section of the health response.
type StorageReport struct {
	HotHours          map[string]int   `json:"hot_hours"`
	ColdFiles         int64            `json:"cold_files"`
	ColdBytes         int64            `json:"cold_bytes"`
	DiskFree          int64            `json:"disk_free"`
	DiskPressure      bool             `json:"disk_pressure"`
	LastFlush         time.Time        `json:"last_flush,omitzero"`
	LastFlushDuration time.Duration    `json:"last_flush_duration_ns"`
	LastFlushError    string           `json:"last_flush_error,omitempty"`
	LastRetention     *RetentionReport `json:"last_retention,omitempty"`
}

// RetentionReport is the last retention run.
type RetentionReport struct {
	At           time.Time `json:"at"`
	FilesDeleted int       `json:"files_deleted"`
	BytesFreed   int64     `json:"bytes_freed"`
	Error        string    `json:"error,omitempty"`
}

type healthResponse struct {
	Status       string         `json:"status"`
	Version      string         `json:"version"`
	Ready        bool           `json:"ready"`
	Ingest       *ingest.Stats  `json:"ingest,omitempty"`
	Storage      *StorageReport `json:"storage,omitempty"`
	StorageError string         `json:"storage_error,omitempty"`
}

// health is unauthenticated: it exposes counts, never telemetry or names.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{Status: "ok", Version: h.info.Version, Ready: true}
	if h.ingestStats != nil {
		st := h.ingestStats.Stats()
		resp.Ingest = &st
	}
	if h.storageStats != nil {
		rep, err := h.storageStats.StorageStats(r.Context())
		if err != nil {
			h.logger.ErrorContext(r.Context(), "storage stats", "err", err)
			resp.Status, resp.StorageError = "degraded", "storage stats unavailable"
		} else {
			resp.Storage = &rep
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
