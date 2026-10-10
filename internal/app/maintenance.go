package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/query"
)

// retentionWait bounds how long retention waits for open queries before
// giving up until the next tick.
const retentionWait = 10 * time.Second

// maintenance is what the maintenance loop works on.
type maintenance struct {
	flusher      *flush.Flusher
	gate         *query.Gate
	policy       flush.Policy
	dataDir      string
	diskFloor    int64
	diskPressure *atomic.Bool
	status       *storageStatus
	logger       *slog.Logger
}

// runMaintenance runs one maintenance pass every interval until ctx ends.
// Failures are logged and retried on the next tick, never fatal.
func runMaintenance(ctx context.Context, m maintenance, interval time.Duration, now func() time.Time) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.run(ctx, now())
		}
	}
}

// run flushes due hours without any lock, since queries on a pinned snapshot
// stay consistent across a flush; applies retention holding the query gate
// exclusively, since it unlinks files a query may be reading; then samples
// free disk.
func (m maintenance) run(ctx context.Context, now time.Time) {
	attrs := []any{}

	start := time.Now()
	flushed, err := m.flusher.FlushDue(ctx, now)
	var rows int64
	for _, st := range flushed {
		rows += st.Rows
	}
	flushDur := time.Since(start)
	m.status.recordFlush(time.Now().UTC(), flushDur, err)
	attrs = append(attrs, "flushed_hours", len(flushed), "flushed_rows", rows, "flush_duration", flushDur)
	if err != nil {
		m.logger.ErrorContext(ctx, "flush", "err", err)
	}

	start = time.Now()
	gateCtx, cancel := context.WithTimeout(ctx, retentionWait)
	release, err := m.gate.Exclusive(gateCtx)
	cancel()
	if err != nil {
		m.logger.WarnContext(ctx, "retention skipped: queries still running", "err", err)
	} else {
		rep, err := m.flusher.ApplyRetention(ctx, m.policy, now)
		release()
		m.status.recordRetention(time.Now().UTC(), rep, err)
		attrs = append(attrs, "retention_files", rep.FilesDeleted, "retention_bytes", rep.BytesFreed,
			"retention_duration", time.Since(start))
		if err != nil {
			m.logger.ErrorContext(ctx, "retention", "err", err)
		}
	}

	if free, err := sampleDisk(m.dataDir, m.diskFloor, m.diskPressure); err != nil {
		m.logger.ErrorContext(ctx, "disk free", "err", err)
	} else {
		attrs = append(attrs, "disk_free", free, "disk_pressure", m.diskPressure.Load())
	}

	m.status.refresh(ctx)
	m.logger.InfoContext(ctx, "maintenance", attrs...)
}

// sampleDisk sets pressure when free disk under dataDir is below floor.
func sampleDisk(dataDir string, floor int64, pressure *atomic.Bool) (int64, error) {
	free, err := flush.DiskFree(dataDir)
	if err != nil {
		return 0, err
	}
	pressure.Store(free < floor)
	return free, nil
}
