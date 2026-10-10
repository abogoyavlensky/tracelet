// Package ingest owns the bounded queue between the OTLP handler and the
// single DuckDB writer, and batches submissions into commits.
package ingest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// Errors Submit returns without waiting for a commit.
var (
	ErrOverloaded = errors.New("ingest queue is full")
	ErrClosed     = errors.New("ingester is shutting down")
)

// Writer commits a batch to the hot tables.
type Writer interface {
	Write(ctx context.Context, batch telemetry.Batch) (duckdb.CommitStats, error)
}

// Config bounds the queue and the commits.
type Config struct {
	// QueueSize is how many submissions may wait; a full queue overloads.
	QueueSize int
	// MaxBatchRows ends a commit once this many rows are pending.
	MaxBatchRows int
	// MaxBatchWait ends a commit this long after its first submission.
	MaxBatchWait time.Duration
}

// DefaultConfig is 256 queued submissions, and commits of up to 2,000 rows or
// 250 ms: about four small commits a second at the normal workload, well
// clear of the multi-second stalls phase 0 measured at 12,000-row commits.
var DefaultConfig = Config{QueueSize: 256, MaxBatchRows: 2000, MaxBatchWait: 250 * time.Millisecond}

type submission struct {
	batch telemetry.Batch
	done  chan error // buffered, receives the commit's result once
}

// Ingester queues submissions and commits them from one goroutine, so ingest
// stamps commit in order. Start Run, submit, then Close.
type Ingester struct {
	w     Writer
	cfg   Config
	queue chan *submission

	// mu guards closed against a Submit sending on the closed queue.
	mu     sync.RWMutex
	closed bool
	done   chan struct{} // closed when Run has drained the queue

	stats counters
}

type counters struct {
	acceptedRows    atomic.Int64
	rejectedRows    atomic.Int64
	unknownService  atomic.Int64
	overloaded      atomic.Int64
	commits         atomic.Int64
	failedCommits   atomic.Int64
	lastCommitNanos atomic.Int64
	lastLatency     atomic.Int64
}

// New returns an ingester writing through w.
func New(w Writer, cfg Config) *Ingester {
	return &Ingester{
		w:     w,
		cfg:   cfg,
		queue: make(chan *submission, cfg.QueueSize),
		done:  make(chan struct{}),
	}
}

// Submit queues batch and waits until its commit completes or ctx ends. It
// returns ErrOverloaded at once when the queue is full. A submission is never
// split across commits, so it lands whole or not at all; if ctx ends first
// the commit may still happen.
func (i *Ingester) Submit(ctx context.Context, batch telemetry.Batch) error {
	if batch.Len() == 0 {
		return nil
	}
	sub := &submission{batch: batch, done: make(chan error, 1)}

	i.mu.RLock()
	if i.closed {
		i.mu.RUnlock()
		return ErrClosed
	}
	select {
	case i.queue <- sub:
		i.mu.RUnlock()
	default:
		i.mu.RUnlock()
		i.stats.overloaded.Add(1)
		return ErrOverloaded
	}

	select {
	case err := <-sub.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RecordRejected counts records the handler rejected before submitting, and
// records filed under the unknown service, for Stats.
func (i *Ingester) RecordRejected(rejected, unknownService int64) {
	i.stats.rejectedRows.Add(rejected)
	i.stats.unknownService.Add(unknownService)
}

// Run commits submissions until Close, then drains what is queued and
// returns. It is the only caller of the writer.
func (i *Ingester) Run(ctx context.Context) {
	defer close(i.done)
	var carry *submission
	for {
		first := carry
		carry = nil
		if first == nil {
			var ok bool
			if first, ok = <-i.queue; !ok {
				return
			}
		}

		pending := []*submission{first}
		rows := first.batch.Len()
		timer := time.NewTimer(i.cfg.MaxBatchWait)
	collect:
		for rows < i.cfg.MaxBatchRows {
			select {
			case sub, ok := <-i.queue:
				if !ok {
					break collect
				}
				if rows+sub.batch.Len() > i.cfg.MaxBatchRows {
					// Too big to join: it starts the next commit, alone if it
					// is oversize on its own.
					carry = sub
					break collect
				}
				pending = append(pending, sub)
				rows += sub.batch.Len()
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()
		i.commit(ctx, pending, rows)
	}
}

func (i *Ingester) commit(ctx context.Context, pending []*submission, rows int) {
	var batch telemetry.Batch
	if len(pending) == 1 {
		batch = pending[0].batch
	} else {
		batch.Logs = make([]telemetry.Log, 0, rows)
		for _, sub := range pending {
			batch.Logs = append(batch.Logs, sub.batch.Logs...)
			batch.Spans = append(batch.Spans, sub.batch.Spans...)
			batch.Points = append(batch.Points, sub.batch.Points...)
		}
	}

	st, err := i.w.Write(ctx, batch)
	if err != nil {
		i.stats.failedCommits.Add(1)
	} else {
		i.stats.commits.Add(1)
		i.stats.acceptedRows.Add(int64(st.Rows))
		i.stats.lastCommitNanos.Store(time.Now().UnixNano())
		i.stats.lastLatency.Store(int64(st.Latency))
	}
	for _, sub := range pending {
		sub.done <- err
	}
}

// Close stops taking submissions and waits until Run has committed every
// queued one. Run must have been started.
func (i *Ingester) Close() {
	i.mu.Lock()
	if !i.closed {
		i.closed = true
		close(i.queue)
	}
	i.mu.Unlock()
	<-i.done
}

// Stats is a point-in-time view of the counters.
type Stats struct {
	AcceptedRows      int64         `json:"accepted_rows"`
	RejectedRows      int64         `json:"rejected_rows"`
	UnknownService    int64         `json:"unknown_service_records"`
	Overloaded        int64         `json:"overloaded_requests"`
	Commits           int64         `json:"commits"`
	FailedCommits     int64         `json:"failed_commits"`
	QueueDepth        int           `json:"queue_depth"`
	LastCommit        time.Time     `json:"last_commit,omitzero"`
	LastCommitLatency time.Duration `json:"last_commit_latency_ns"`
}

// Stats returns the current counters.
func (i *Ingester) Stats() Stats {
	s := Stats{
		AcceptedRows:      i.stats.acceptedRows.Load(),
		RejectedRows:      i.stats.rejectedRows.Load(),
		UnknownService:    i.stats.unknownService.Load(),
		Overloaded:        i.stats.overloaded.Load(),
		Commits:           i.stats.commits.Load(),
		FailedCommits:     i.stats.failedCommits.Load(),
		QueueDepth:        len(i.queue),
		LastCommitLatency: time.Duration(i.stats.lastLatency.Load()),
	}
	if n := i.stats.lastCommitNanos.Load(); n != 0 {
		s.LastCommit = time.Unix(0, n).UTC()
	}
	return s
}
