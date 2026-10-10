package ingest_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/ingest"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// fakeWriter records the row count of every commit. When gate is set, each
// Write waits for a value on it first.
type fakeWriter struct {
	mu      sync.Mutex
	commits []int
	err     error
	gate    chan struct{}
	entered chan struct{} // receives once per Write, when set
}

func (f *fakeWriter) Write(_ context.Context, b telemetry.Batch) (duckdb.CommitStats, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return duckdb.CommitStats{}, f.err
	}
	f.commits = append(f.commits, b.Len())
	return duckdb.CommitStats{Rows: b.Len(), Latency: time.Millisecond}, nil
}

func (f *fakeWriter) Commits() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.commits...)
}

func logs(n int) telemetry.Batch {
	b := telemetry.Batch{Logs: make([]telemetry.Log, n)}
	for i := range b.Logs {
		b.Logs[i] = telemetry.Log{TS: time.Now(), Project: "p1", Service: "api"}
	}
	return b
}

// start runs an ingester and closes it at cleanup.
func start(t *testing.T, w ingest.Writer, cfg ingest.Config) *ingest.Ingester {
	t.Helper()
	in := ingest.New(w, cfg)
	var wg sync.WaitGroup
	wg.Go(func() { in.Run(context.Background()) })
	t.Cleanup(func() {
		in.Close()
		wg.Wait()
	})
	return in
}

func TestSubmitWaitsForCommit(t *testing.T) {
	w := &fakeWriter{}
	in := start(t, w, ingest.DefaultConfig)

	require.NoError(t, in.Submit(t.Context(), logs(3)))
	assert.Equal(t, []int{3}, w.Commits())

	st := in.Stats()
	assert.Equal(t, int64(3), st.AcceptedRows)
	assert.Equal(t, int64(1), st.Commits)
	assert.False(t, st.LastCommit.IsZero())
	assert.Equal(t, time.Millisecond, st.LastCommitLatency)
}

func TestConcurrentSubmitsShareCommits(t *testing.T) {
	w := &fakeWriter{}
	in := start(t, w, ingest.DefaultConfig)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(100))) })
	}
	wg.Wait()

	commits := w.Commits()
	assert.LessOrEqual(t, len(commits), 2, "five submits within the wait share commits: %v", commits)
	total := 0
	for _, n := range commits {
		total += n
	}
	assert.Equal(t, 500, total)
}

func TestOversizeSubmissionCommitsAlone(t *testing.T) {
	w := &fakeWriter{gate: make(chan struct{}), entered: make(chan struct{}, 10)}
	in := start(t, w, ingest.Config{QueueSize: 16, MaxBatchRows: 2000, MaxBatchWait: 50 * time.Millisecond})

	// Hold the writer on a first commit so the rest queue up behind it.
	var wg sync.WaitGroup
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(1))) })
	<-w.entered
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(10))) })
	require.Eventually(t, func() bool { return in.Stats().QueueDepth == 1 }, time.Second, time.Millisecond)
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(5000))) })
	require.Eventually(t, func() bool { return in.Stats().QueueDepth == 2 }, time.Second, time.Millisecond)
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(20))) })
	require.Eventually(t, func() bool { return in.Stats().QueueDepth == 3 }, time.Second, time.Millisecond)

	go func() {
		for range 4 {
			w.gate <- struct{}{}
		}
	}()
	wg.Wait()

	commits := w.Commits()
	assert.Contains(t, commits, 5000, "the oversize submission is one commit of its own")
	for _, n := range commits {
		assert.True(t, n == 5000 || n <= 2000, "nothing is merged into the oversize commit: %v", commits)
	}
	assert.Equal(t, int64(5031), in.Stats().AcceptedRows)
}

func TestWriterErrorReachesEverySubmitter(t *testing.T) {
	boom := errors.New("disk full")
	w := &fakeWriter{err: boom}
	in := start(t, w, ingest.DefaultConfig)

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { assert.ErrorIs(t, in.Submit(t.Context(), logs(10)), boom) })
	}
	wg.Wait()
	assert.Zero(t, in.Stats().AcceptedRows)
	assert.Positive(t, in.Stats().FailedCommits)
}

func TestFullQueueOverloads(t *testing.T) {
	w := &fakeWriter{gate: make(chan struct{}), entered: make(chan struct{}, 10)}
	in := start(t, w, ingest.Config{QueueSize: 1, MaxBatchRows: 1, MaxBatchWait: time.Millisecond})

	var wg sync.WaitGroup
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(1))) })
	<-w.entered // the writer holds the first submission
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(1))) })
	require.Eventually(t, func() bool { return in.Stats().QueueDepth == 1 }, time.Second, time.Millisecond)

	start := time.Now()
	require.ErrorIs(t, in.Submit(t.Context(), logs(1)), ingest.ErrOverloaded)
	assert.Less(t, time.Since(start), 50*time.Millisecond)
	assert.Equal(t, int64(1), in.Stats().Overloaded)

	w.gate <- struct{}{}
	w.gate <- struct{}{}
	wg.Wait()
}

func TestCloseDrainsQueue(t *testing.T) {
	w := &fakeWriter{gate: make(chan struct{}), entered: make(chan struct{}, 10)}
	in := ingest.New(w, ingest.Config{QueueSize: 8, MaxBatchRows: 1, MaxBatchWait: time.Millisecond})
	var run sync.WaitGroup
	run.Go(func() { in.Run(context.Background()) })

	var wg sync.WaitGroup
	wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(1))) })
	<-w.entered
	for range 3 {
		wg.Go(func() { assert.NoError(t, in.Submit(t.Context(), logs(1))) })
	}
	require.Eventually(t, func() bool { return in.Stats().QueueDepth == 3 }, time.Second, time.Millisecond)

	closed := make(chan struct{})
	go func() {
		in.Close()
		close(closed)
	}()
	go func() {
		for range time.Tick(time.Millisecond) {
			select {
			case w.gate <- struct{}{}:
			case <-closed:
				return
			}
		}
	}()

	// Poll with a cancelled context so a submission that slips in before
	// Close does not block; count those, since they must be committed too.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	extra := 0
	require.Eventually(t, func() bool {
		err := in.Submit(cancelled, logs(1))
		if err == nil || errors.Is(err, context.Canceled) {
			extra++
		}
		return errors.Is(err, ingest.ErrClosed)
	}, time.Second, time.Millisecond, "intake stops at Close")

	<-closed
	wg.Wait()
	run.Wait()
	assert.Len(t, w.Commits(), 4+extra, "every queued submission was committed")
}

func TestRecordRejected(t *testing.T) {
	in := start(t, &fakeWriter{}, ingest.DefaultConfig)
	in.RecordRejected(2, 3)
	st := in.Stats()
	assert.Equal(t, int64(2), st.RejectedRows)
	assert.Equal(t, int64(3), st.UnknownService)
}
