package scenario

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/report"
	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

// memorySampler records the process's memory on the wall clock while a run
// goes on. Flushes are synchronous in the run loop, so sampling from the loop
// would miss everything allocated and freed inside a flush; a goroutine does
// not. The runner owns it: startMemorySampler starts the goroutine, stop
// cancels it and waits, and failed samples are counted, never fatal.
type memorySampler struct {
	// Written by the run loop, read by the goroutine.
	simTime atomic.Int64 // UnixNano of the second being generated
	inFlush atomic.Bool

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	// Owned by the goroutine until done is closed.
	samples []report.MemorySample
	peak    int // index into samples of the highest RSS, -1 when none
	errors  int
}

// startMemorySampler takes one sample every interval on its own connection
// until stop is called.
func startMemorySampler(ctx context.Context, s *store.Store, interval time.Duration) (*memorySampler, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("memory sample interval must be positive, got %s", interval)
	}
	conn, err := s.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory sampler connection: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &memorySampler{cancel: cancel, done: make(chan struct{}), peak: -1}
	go func() {
		defer close(m.done)
		defer conn.Close()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.take(ctx, conn)
			}
		}
	}()
	return m, nil
}

func (m *memorySampler) take(ctx context.Context, conn *sql.Conn) {
	inFlush := m.inFlush.Load()
	rss, hwm, err := report.ReadProcMemory()
	if err != nil {
		m.errors++
		return
	}
	goMem := report.GoMemory()
	duck, err := store.MemoryByTag(ctx, conn)
	if err != nil {
		// The run ending mid-query is not a failed sample.
		if ctx.Err() == nil {
			m.errors++
		}
		return
	}
	var duckTotal int64
	for _, n := range duck.ByTag {
		duckTotal += n
	}
	m.samples = append(m.samples, report.MemorySample{
		SimTime:    time.Unix(0, m.simTime.Load()).UTC(),
		Wall:       time.Now().UTC(),
		RSS:        rss,
		HWM:        hwm,
		GoResident: goMem.Resident,
		GoHeapUsed: goMem.HeapInuse,
		DuckDB:     duck.ByTag,
		DuckDBTemp: duck.Temp,
		Residual:   rss - goMem.Resident - duckTotal,
		// A flush running at either end of the sample counts.
		InFlush: inFlush || m.inFlush.Load(),
	})
	if m.peak < 0 || rss > m.samples[m.peak].RSS {
		m.peak = len(m.samples) - 1
	}
}

// stop cancels the goroutine and waits for it. It is safe to call more than
// once; the result is only meaningful after the first call returns.
func (m *memorySampler) stop() (samples []report.MemorySample, peak *report.MemorySample, errors int) {
	m.once.Do(func() {
		m.cancel()
		<-m.done
	})
	if m.peak >= 0 {
		p := m.samples[m.peak]
		peak = &p
	}
	return m.samples, peak, m.errors
}
