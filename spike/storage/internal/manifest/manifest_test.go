package manifest_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/manifest"
)

var h0 = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

func openManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Open(filepath.Join(t.TempDir(), "manifest.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func file(signal string, hour time.Time, path string, bytes int64) manifest.File {
	return manifest.File{
		Signal: signal, Hour: hour, Path: path, Rows: 10,
		MinTS:       hour.Add(time.Minute),
		MaxTS:       hour.Add(59*time.Minute + 123456*time.Microsecond),
		MaxIngestTS: hour.Add(2*time.Hour + time.Microsecond),
		Bytes:       bytes,
	}
}

func paths(files []manifest.File) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestAddAndQuery(t *testing.T) {
	m := openManifest(t)
	ctx := t.Context()
	require.NoError(t, m.Add(ctx, file("logs", h0, "a.parquet", 100)))
	require.NoError(t, m.Add(ctx, file("logs", h0.Add(time.Hour), "b.parquet", 200)))
	require.NoError(t, m.Add(ctx, file("spans", h0, "c.parquet", 50)))

	got, err := m.FilesFor(ctx, "logs", h0.Add(time.Hour), h0.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []string{"b.parquet"}, paths(got))

	got, err = m.FilesFor(ctx, "logs", h0, h0.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []string{"a.parquet", "b.parquet"}, paths(got))

	want := file("logs", h0, "a.parquet", 100)
	assert.Equal(t, want.MaxTS, got[0].MaxTS, "microseconds survive the round trip")
	assert.Equal(t, want.MaxIngestTS, got[0].MaxIngestTS)
	assert.Equal(t, h0, got[0].Hour)
	assert.Equal(t, manifest.SchemaVersion, got[0].SchemaVersion)

	hours, err := m.Hours(ctx, "logs")
	require.NoError(t, err)
	assert.Equal(t, []time.Time{h0, h0.Add(time.Hour)}, hours)

	total, err := m.TotalBytes(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(350), total)

	hour, bytes, ok, err := m.OldestHour(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, h0, hour)
	assert.Equal(t, int64(150), bytes)

	require.NoError(t, m.Remove(ctx, "a.parquet"))
	hours, err = m.Hours(ctx, "logs")
	require.NoError(t, err)
	assert.Equal(t, []time.Time{h0.Add(time.Hour)}, hours)
}

func TestFilesForUsesRealRange(t *testing.T) {
	m := openManifest(t)
	// A late-data file of hour 0 whose rows lie in hour 0 is found by a query
	// over hour 0, and not by one over hour 1.
	require.NoError(t, m.Add(t.Context(), file("logs", h0, "late.parquet", 1)))

	got, err := m.FilesFor(t.Context(), "logs", h0.Add(30*time.Minute), h0.Add(31*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []string{"late.parquet"}, paths(got))

	got, err = m.FilesFor(t.Context(), "logs", h0.Add(time.Hour), h0.Add(2*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestAddDuplicate(t *testing.T) {
	m := openManifest(t)
	require.NoError(t, m.Add(t.Context(), file("logs", h0, "a.parquet", 1)))
	err := m.Add(t.Context(), file("logs", h0, "a.parquet", 1))
	assert.ErrorIs(t, err, manifest.ErrDuplicate)
}

func TestEmptyManifest(t *testing.T) {
	m := openManifest(t)
	_, _, ok, err := m.OldestHour(t.Context())
	require.NoError(t, err)
	assert.False(t, ok)

	_, ok, err = m.MaxIngestCutoff(t.Context(), "logs", h0)
	require.NoError(t, err)
	assert.False(t, ok)

	total, err := m.TotalBytes(t.Context())
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestMaxIngestCutoff(t *testing.T) {
	m := openManifest(t)
	first := file("logs", h0, "a.parquet", 1)
	second := file("logs", h0, "b.parquet", 1)
	second.MaxIngestTS = first.MaxIngestTS.Add(time.Hour)
	require.NoError(t, m.Add(t.Context(), first))
	require.NoError(t, m.Add(t.Context(), second))

	cutoff, ok, err := m.MaxIngestCutoff(t.Context(), "logs", h0)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, second.MaxIngestTS, cutoff)
}
