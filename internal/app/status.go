package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
)

// storageStatus remembers the maintenance loop's last results and reports
// them, with live storage counts, to the health endpoint.
type storageStatus struct {
	store        *duckdb.Store
	manifest     *manifest.Manifest
	dataDir      string
	diskPressure *atomic.Bool

	mu            sync.Mutex
	lastFlush     time.Time
	lastFlushDur  time.Duration
	lastFlushErr  string
	lastRetention *httpapi.RetentionReport
}

func (s *storageStatus) recordFlush(at time.Time, dur time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFlush, s.lastFlushDur, s.lastFlushErr = at, dur, errString(err)
}

func (s *storageStatus) recordRetention(at time.Time, rep flush.RetentionReport, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRetention = &httpapi.RetentionReport{
		At: at, FilesDeleted: rep.FilesDeleted, BytesFreed: rep.BytesFreed, Error: errString(err),
	}
}

// StorageStats reads hot hours per signal, cold totals, and free disk.
func (s *storageStatus) StorageStats(ctx context.Context) (httpapi.StorageReport, error) {
	rep := httpapi.StorageReport{HotHours: map[string]int{}, DiskPressure: s.diskPressure.Load()}
	for _, signal := range duckdb.Signals {
		hours, err := s.store.HotHours(ctx, signal)
		if err != nil {
			return rep, err
		}
		rep.HotHours[signal] = len(hours)
	}
	var err error
	if rep.ColdFiles, rep.ColdBytes, err = s.manifest.Totals(ctx); err != nil {
		return rep, err
	}
	if rep.DiskFree, err = flush.DiskFree(s.dataDir); err != nil {
		return rep, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rep.LastFlush, rep.LastFlushDuration, rep.LastFlushError = s.lastFlush, s.lastFlushDur, s.lastFlushErr
	if s.lastRetention != nil {
		r := *s.lastRetention
		rep.LastRetention = &r
	}
	return rep, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
