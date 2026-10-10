package app

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
)

// storageStatus remembers the maintenance loop's last results and the
// storage counts it last took, and reports them to the health endpoint.
// Counts are refreshed by maintenance, not per request, so an
// unauthenticated health check never scans the hot tables.
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
	counts        httpapi.StorageReport
	countsErr     error
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

// refresh takes the storage counts: hot hours per signal, cold totals, and
// free disk.
func (s *storageStatus) refresh(ctx context.Context) {
	counts, err := s.count(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts, s.countsErr = counts, err
}

func (s *storageStatus) count(ctx context.Context) (httpapi.StorageReport, error) {
	rep := httpapi.StorageReport{HotHours: map[string]int{}}
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
	return rep, nil
}

// StorageStats returns the last counts with the last maintenance results. It
// fails when the last refresh did.
func (s *storageStatus) StorageStats(context.Context) (httpapi.StorageReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.countsErr != nil {
		return httpapi.StorageReport{}, s.countsErr
	}
	rep := s.counts
	rep.HotHours = maps.Clone(s.counts.HotHours)
	rep.DiskPressure = s.diskPressure.Load()
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
