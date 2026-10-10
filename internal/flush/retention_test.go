package flush_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/flush"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
)

const fakeFileBytes = 1000

// fakeHours records one fake file per signal for each of five hours. Retention
// never reads the files, so their content does not matter.
func fakeHours(t *testing.T, e *env, signals ...string) {
	t.Helper()
	for i := range 5 {
		hour := h0.Add(time.Duration(i) * time.Hour)
		for _, signal := range signals {
			rel := filepath.ToSlash(filepath.Join(flush.HourDir(signal, hour), "f.parquet"))
			path := filepath.Join(e.DataDir, rel)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, make([]byte, fakeFileBytes), 0o644))
			require.NoError(t, e.Manifest.Add(t.Context(), manifest.File{
				Signal: signal, Hour: hour, Path: rel, Rows: 1,
				MinTS: hour, MaxTS: hour.Add(time.Minute), MaxIngestTS: hour, Bytes: fakeFileBytes,
			}))
		}
	}
}

func hours(t *testing.T, e *env, signal string) []time.Time {
	t.Helper()
	hs, err := e.Manifest.Hours(t.Context(), signal)
	require.NoError(t, err)
	return hs
}

func TestRetentionByAge(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	fakeHours(t, e, "logs", "spans")

	rep, err := e.Flusher.ApplyRetention(t.Context(), flush.Policy{Days: map[string]int{"logs": 0}}, h0.Add(5*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 5, rep.HoursDeleted)
	assert.Equal(t, int64(5*fakeFileBytes), rep.BytesFreed)
	assert.Empty(t, hours(t, e, "logs"))
	assert.Len(t, hours(t, e, "spans"), 5, "other signals keep their age")
	assert.NoDirExists(t, filepath.Join(e.DataDir, flush.HourDir("logs", h0)))
	assert.FileExists(t, filepath.Join(e.DataDir, flush.HourDir("spans", h0), "f.parquet"))
}

func TestRetentionByAgeKeepsRecentHours(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	fakeHours(t, e, "logs")

	// One day of retention at h0 + 1 day + 2h keeps the hours ending after h0 + 2h.
	_, err := e.Flusher.ApplyRetention(t.Context(), flush.Policy{Days: map[string]int{"logs": 1}}, h0.Add(26*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []time.Time{h0.Add(2 * time.Hour), h0.Add(3 * time.Hour), h0.Add(4 * time.Hour)}, hours(t, e, "logs"))
}

func TestRetentionRingBuffer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	fakeHours(t, e, "logs", "spans")

	// Ten files of 1000 bytes; a 4500-byte cap keeps the newest two hours.
	rep, err := e.Flusher.ApplyRetention(t.Context(), flush.Policy{MaxBytes: 4500}, h0.Add(5*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 3, rep.HoursDeleted)
	assert.Equal(t, 6, rep.FilesDeleted)
	want := []time.Time{h0.Add(3 * time.Hour), h0.Add(4 * time.Hour)}
	assert.Equal(t, want, hours(t, e, "logs"))
	assert.Equal(t, want, hours(t, e, "spans"))

	total, err := e.Manifest.TotalBytes(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(4000), total)
	for i := range 3 {
		hour := h0.Add(time.Duration(i) * time.Hour)
		assert.NoDirExists(t, filepath.Join(e.DataDir, flush.HourDir("logs", hour)))
	}
	// Every hour shared one date, which still holds the surviving hours.
	assert.DirExists(t, filepath.Join(e.DataDir, flush.HourDir("logs", h0.Add(4*time.Hour))))
}

func TestRetentionRemovesEmptyDateDir(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	fakeHours(t, e, "logs")

	_, err := e.Flusher.ApplyRetention(t.Context(), flush.Policy{Days: map[string]int{"logs": 0}}, h0.Add(5*time.Hour))
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Dir(filepath.Join(e.DataDir, flush.HourDir("logs", h0))))
	assert.DirExists(t, filepath.Join(e.DataDir, flush.TelemetryDir, "logs"))
}

func TestInterruptedRetentionCompletesOnReconcile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	fakeHours(t, e, "logs")
	ctx := t.Context()

	e.Flusher.Hook = func(step string) error {
		if step == flush.StepRetentionRemoved {
			return errors.New("crash")
		}
		return nil
	}
	_, err := e.Flusher.ApplyRetention(ctx, flush.Policy{MaxBytes: 4500}, h0.Add(5*time.Hour))
	require.ErrorContains(t, err, "hook at "+flush.StepRetentionRemoved)
	assert.Contains(t, hours(t, e, "logs"), h0, "the dangling row is still there")

	e.Flusher.Hook = nil
	rep, err := e.Flusher.Reconcile(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Dropped)
	assert.NotContains(t, hours(t, e, "logs"), h0, "the hour does not come back")
	assert.NoFileExists(t, filepath.Join(e.DataDir, flush.HourDir("logs", h0), "f.parquet"))
}

func TestDiskFree(t *testing.T) {
	t.Parallel()
	free, err := flush.DiskFree(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, free)
}
