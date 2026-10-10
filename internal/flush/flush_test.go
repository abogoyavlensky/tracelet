package flush_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

var (
	h0 = start
	h1 = h0.Add(time.Hour)
	h2 = h0.Add(2 * time.Hour)
	h3 = h0.Add(3 * time.Hour)
)

// lateSeq is beyond any seq ingest3h writes.
const lateSeq = 1 << 40

func flushedHours(stats []flush.Stats) map[string][]time.Time {
	out := map[string][]time.Time{}
	for _, s := range stats {
		out[s.Signal] = append(out[s.Signal], s.Hour)
	}
	return out
}

func hotStats(t *testing.T, e *env, signal string, from, to time.Time) (rows int64, minTS, maxTS, maxIngest time.Time) {
	t.Helper()
	err := e.Store.DB().QueryRow(
		"SELECT count(*), min(ts), max(ts), max(ingest_ts) FROM "+signal+" WHERE ts >= ? AND ts < ?", from, to,
	).Scan(&rows, &minTS, &maxTS, &maxIngest)
	require.NoError(t, err)
	return rows, minTS.UTC(), maxTS.UTC(), maxIngest.UTC()
}

func TestFlushDueFlushesClosedHours(t *testing.T) {
	t.Parallel()
	e, _ := ingest3h(t)
	ctx := t.Context()

	type want struct {
		rows                      int64
		minTS, maxTS, maxIngestTS time.Time
	}
	expected := map[string]want{}
	for _, signal := range duckdb.Signals {
		var w want
		w.rows, w.minTS, w.maxTS, w.maxIngestTS = hotStats(t, e, signal, h0, h1)
		require.Positive(t, w.rows)
		expected[signal] = w
	}

	stats, err := e.Flusher.FlushDue(ctx, h2.Add(59*time.Minute))
	require.NoError(t, err)

	for _, signal := range duckdb.Signals {
		assert.Equal(t, []time.Time{h0}, flushedHours(stats)[signal], signal)
		assert.Zero(t, e.hotCount(t, signal, h0, h1), "hour 0 left hot for %s", signal)
		assert.Positive(t, e.hotCount(t, signal, h1, h2), "hour 1 stays hot for %s", signal)
	}

	files, err := e.Manifest.All(ctx)
	require.NoError(t, err)
	require.Len(t, files, 3)
	pathRE := regexp.MustCompile(`^telemetry/(logs|spans|metric_points)/date=2026-10-01/hour=00/\d+-[0-9a-f]{16}\.parquet$`)
	for _, f := range files {
		w := expected[f.Signal]
		assert.Regexp(t, pathRE, f.Path)
		assert.Equal(t, h0, f.Hour)
		assert.Equal(t, w.rows, f.Rows, f.Signal)
		assert.Equal(t, w.minTS, f.MinTS, f.Signal)
		assert.Equal(t, w.maxTS, f.MaxTS, f.Signal)
		assert.Equal(t, w.maxIngestTS, f.MaxIngestTS, f.Signal)

		info, err := os.Stat(filepath.Join(e.DataDir, f.Path))
		require.NoError(t, err)
		assert.Equal(t, info.Size(), f.Bytes)
	}
}

func TestFlushDueAtHourBoundary(t *testing.T) {
	t.Parallel()
	e, _ := ingest3h(t)

	stats, err := e.Flusher.FlushDue(t.Context(), h3)
	require.NoError(t, err)

	for _, signal := range duckdb.Signals {
		assert.Equal(t, []time.Time{h0, h1}, flushedHours(stats)[signal], signal)
	}
}

func TestLateRowsFlushIntoSecondFile(t *testing.T) {
	t.Parallel()
	e, rows := ingest3h(t)
	ctx := t.Context()

	_, err := e.Flusher.FlushDue(ctx, h3)
	require.NoError(t, err)
	first, err := e.Manifest.FilesFor(ctx, "logs", h0, h1)
	require.NoError(t, err)
	require.Len(t, first, 1)

	e.write(t, telemetry.Batch{Logs: []telemetry.Log{logAt(h0.Add(30*time.Minute), lateSeq)}}, rows)
	_, err = e.Flusher.FlushDue(ctx, h3)
	require.NoError(t, err)

	files, err := e.Manifest.FilesFor(ctx, "logs", h0, h1)
	require.NoError(t, err)
	require.Len(t, files, 2)
	var second manifest.File
	for _, f := range files {
		if f.Path != first[0].Path {
			second = f
		}
	}
	assert.Equal(t, int64(1), second.Rows)
	assert.True(t, second.MaxIngestTS.After(first[0].MaxIngestTS))
	assert.Zero(t, e.hotCount(t, "logs", h0, h1))
}

func TestCutoffScopesDelete(t *testing.T) {
	t.Parallel()
	e, rows := ingest3h(t)

	// A row for hour 0 committed after the first export, before its delete.
	written := false
	e.Flusher.Hook = func(step string) error {
		if step == flush.StepWritten && !written {
			written = true
			e.write(t, telemetry.Batch{Logs: []telemetry.Log{logAt(h0.Add(time.Minute), lateSeq)}}, rows)
		}
		return nil
	}
	_, err := e.Flusher.FlushDue(t.Context(), h2)
	require.NoError(t, err)

	assert.Equal(t, int64(1), e.hotCount(t, "logs", h0, h1))
}

func TestFlushAllEmptiesHot(t *testing.T) {
	t.Parallel()
	e, rows := ingest3h(t)

	stats, err := e.Flusher.FlushAll(t.Context())
	require.NoError(t, err)
	assert.Len(t, stats, 9)

	var flushed int64
	for _, s := range stats {
		flushed += s.Rows
	}
	assert.Equal(t, rows["logs"]+rows["spans"]+rows["metric_points"], flushed)
	for _, signal := range duckdb.Signals {
		assert.Zero(t, e.hotCount(t, signal, h0, h3.Add(time.Hour)), signal)
	}
}

func TestHookErrorAtWrittenLeavesTmp(t *testing.T) {
	t.Parallel()
	e, _ := ingest3h(t)
	before := e.hotCount(t, "logs", h0, h1)

	e.Flusher.Hook = func(step string) error {
		if step == flush.StepWritten {
			return errors.New("boom")
		}
		return nil
	}
	_, err := e.Flusher.FlushDue(t.Context(), h2)
	require.ErrorContains(t, err, "hook at written")

	assert.Len(t, filesWithSuffix(t, e.DataDir, ".parquet.tmp"), 1)
	assert.Empty(t, filesWithSuffix(t, e.DataDir, ".parquet"))
	files, err := e.Manifest.All(t.Context())
	require.NoError(t, err)
	assert.Empty(t, files)
	assert.Equal(t, before, e.hotCount(t, "logs", h0, h1))
}

func TestRetryAfterFailedDeleteDoesNotDuplicate(t *testing.T) {
	t.Parallel()
	e, _ := ingest3h(t)
	ctx := t.Context()
	hot := e.hotCount(t, "logs", h0, h1)

	e.Flusher.Hook = func(step string) error {
		if step == flush.StepRecorded {
			return errors.New("delete never ran")
		}
		return nil
	}
	_, err := e.Flusher.FlushDue(ctx, h2)
	require.Error(t, err)

	e.Flusher.Hook = nil
	_, err = e.Flusher.FlushDue(ctx, h2)
	require.NoError(t, err)

	files, err := e.Manifest.FilesFor(ctx, "logs", h0, h1)
	require.NoError(t, err)
	var cold int64
	for _, f := range files {
		cold += f.Rows
	}
	assert.Equal(t, hot, cold, "every hour-0 log is in cold exactly once")
	assert.Zero(t, e.hotCount(t, "logs", h0, h1))
}

func TestReconcileAfterCrashAtEachStep(t *testing.T) {
	t.Parallel()
	for _, step := range []string{flush.StepWritten, flush.StepRenamed, flush.StepRecorded, flush.StepDeleted} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			e, rows := ingest3h(t)
			ctx := t.Context()

			e.Flusher.Hook = func(s string) error {
				if s == step {
					return errors.New("crash")
				}
				return nil
			}
			_, err := e.Flusher.FlushDue(ctx, h3)
			require.ErrorContains(t, err, "hook at "+step)

			// Rows for the crashed hour that arrive after the export are not in
			// any file, so reconcile must leave them alone.
			e.Flusher.Hook = nil
			e.write(t, telemetry.Batch{Logs: []telemetry.Log{logAt(h0.Add(10*time.Minute), lateSeq)}}, rows)

			// Restart.
			dataDir := e.DataDir
			e.close()
			e = openEnv(t, dataDir)

			_, err = e.Flusher.Reconcile(ctx)
			require.NoError(t, err)
			bySignal, missing, unrecorded := e.accounting(t)

			for _, signal := range duckdb.Signals {
				c := bySignal[signal]
				assert.Equal(t, rows[signal], c.Total, "%s: nothing lost", signal)
				assert.Equal(t, rows[signal], c.DistinctKeys, "%s: nothing duplicated", signal)
				assert.Equal(t, c.Hot+c.Cold, c.Total, signal)
			}
			assert.Empty(t, missing)
			assert.Empty(t, unrecorded)
			assert.Empty(t, filesWithSuffix(t, dataDir, ".parquet.tmp"))
		})
	}
}

func TestReconcileReportsRepairs(t *testing.T) {
	t.Parallel()
	e, _ := ingest3h(t)
	ctx := t.Context()

	e.Flusher.Hook = func(s string) error {
		if s == flush.StepRenamed {
			return errors.New("crash")
		}
		return nil
	}
	_, err := e.Flusher.FlushDue(ctx, h2)
	require.Error(t, err)

	rep, err := e.Flusher.Reconcile(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Adopted)
	assert.Positive(t, rep.HotRowsDeleted)

	files, err := e.Manifest.All(ctx)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, h0, files[0].Hour)
	assert.Equal(t, "logs", files[0].Signal)
}
