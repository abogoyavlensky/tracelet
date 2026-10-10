package query_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

var (
	h0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	h1 = h0.Add(time.Hour)
	h2 = h0.Add(2 * time.Hour)
)

// env is one data dir with the store, manifest, writer, flusher, and gate.
type env struct {
	dataDir  string
	store    *duckdb.Store
	manifest *manifest.Manifest
	writer   *duckdb.Writer
	flusher  *flush.Flusher
	gate     *query.Gate
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	s, err := duckdb.Open(dir, duckdb.Limits{MemoryLimit: "256MB", Threads: 2})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	db, err := sqlite.Open(filepath.Join(dir, "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	w, err := duckdb.NewWriter(t.Context(), s, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	m := manifest.New(db)
	return &env{
		dataDir:  dir,
		store:    s,
		manifest: m,
		writer:   w,
		flusher:  &flush.Flusher{Store: s, Manifest: m, DataDir: dir},
		gate:     query.NewGate(4),
	}
}

// writeLogs writes n logs per minute-spaced timestamp from start, in one batch.
func (e *env) writeLogs(t *testing.T, from time.Time, n int) {
	t.Helper()
	var b telemetry.Batch
	for i := range n {
		b.Logs = append(b.Logs, telemetry.Log{
			TS: from.Add(time.Duration(i) * time.Minute), Project: "p1", Service: "api",
			SeverityNumber: 9, SeverityText: "INFO", Body: fmt.Sprintf("log %d", i),
		})
	}
	_, err := e.writer.Write(t.Context(), b)
	require.NoError(t, err)
}

func (e *env) open(t *testing.T) *query.Snapshot {
	t.Helper()
	snap, err := query.Open(t.Context(), e.store, e.manifest, e.gate, e.dataDir)
	require.NoError(t, err)
	t.Cleanup(snap.Close)
	return snap
}

func count(t *testing.T, snap *query.Snapshot, from, to time.Time) int {
	t.Helper()
	src, args, err := snap.Source(t.Context(), "logs", from, to)
	require.NoError(t, err)
	var n int
	require.NoError(t, snap.QueryRowContext(t.Context(), "SELECT count(*) FROM "+src, args...).Scan(&n))
	return n
}

func TestSnapshotDuringFlushBetweenRecordAndDelete(t *testing.T) {
	e := newEnv(t)
	e.writeLogs(t, h0, 40)
	e.writeLogs(t, h1, 30)

	recorded := make(chan struct{})
	resume := make(chan struct{})
	e.flusher.Hook = func(step string) error {
		if step == flush.StepRecorded {
			close(recorded)
			<-resume
		}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.flusher.FlushDue(context.Background(), h2)
		done <- err
	}()

	<-recorded
	// The file is in the manifest and its rows are still hot.
	snap := e.open(t)
	assert.Equal(t, 40, count(t, snap, h0, h1))
	assert.Equal(t, 70, count(t, snap, h0, h2))
	snap.Close()

	close(resume)
	require.NoError(t, <-done)
	assert.Equal(t, 40, count(t, e.open(t), h0, h1))
}

func TestSnapshotOpenedBeforeFlush(t *testing.T) {
	e := newEnv(t)
	e.writeLogs(t, h0, 40)
	e.writeLogs(t, h1, 30)

	snap := e.open(t)
	_, err := e.flusher.FlushAll(t.Context())
	require.NoError(t, err)

	// The snapshot still sees the deleted hot rows, and the manifest it reads
	// now lists the files holding them: each row counts once.
	var hot int
	require.NoError(t, snap.QueryRowContext(t.Context(), "SELECT count(*) FROM logs").Scan(&hot))
	assert.Equal(t, 70, hot, "the snapshot is pinned at Open")
	assert.Equal(t, 40, count(t, snap, h0, h1))
	assert.Equal(t, 70, count(t, snap, h0, h2))
}

func TestSnapshotCountsLateRowsOnce(t *testing.T) {
	e := newEnv(t)
	e.writeLogs(t, h0, 40)
	_, err := e.flusher.FlushDue(t.Context(), h2)
	require.NoError(t, err)

	// Late rows for the flushed hour are stamped above its file's cutoff.
	e.writeLogs(t, h0.Add(30*time.Second), 5)
	assert.Equal(t, 45, count(t, e.open(t), h0, h1))
}

func TestSourceRejectsUnknownSignal(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.open(t).Source(t.Context(), "logs; DROP TABLE logs", h0, h1)
	require.Error(t, err)
}

func TestGateExclusiveWaitsForSnapshot(t *testing.T) {
	gate := query.NewGate(4)
	release, err := gate.Acquire(t.Context())
	require.NoError(t, err)

	got := make(chan struct{})
	go func() {
		releaseAll, err := gate.Exclusive(context.Background())
		if err == nil {
			releaseAll()
		}
		close(got)
	}()

	select {
	case <-got:
		t.Fatal("exclusive acquired while a snapshot was open")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("exclusive not acquired after the snapshot closed")
	}
}

func TestGateFullAndCancelled(t *testing.T) {
	gate := query.NewGate(4)
	for range 4 {
		release, err := gate.Acquire(t.Context())
		require.NoError(t, err)
		t.Cleanup(release)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	_, err := gate.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 50*time.Millisecond)
}

func TestGateExclusiveDeadline(t *testing.T) {
	gate := query.NewGate(4)
	release, err := gate.Acquire(t.Context())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = gate.Exclusive(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// A timed-out Exclusive gives back the slots it took.
	for range 3 {
		r, err := gate.Acquire(t.Context())
		require.NoError(t, err)
		defer r()
	}
}

func TestOpenWithCancelledContextWhenFull(t *testing.T) {
	e := newEnv(t)
	for range 4 {
		e.open(t)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := query.Open(ctx, e.store, e.manifest, e.gate, e.dataDir)
	require.ErrorIs(t, err, context.Canceled)
}
