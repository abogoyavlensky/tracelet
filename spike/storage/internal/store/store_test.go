package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), store.Limits{MemoryLimit: "64MB", Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesLimits(t *testing.T) {
	s := openStore(t)

	var memory, threads string
	err := s.DB().QueryRow("SELECT current_setting('memory_limit'), current_setting('threads')").Scan(&memory, &threads)
	require.NoError(t, err)

	// DuckDB reads "MB" as 10^6 bytes and reports in binary units.
	assert.Equal(t, "61.0 MiB", memory)
	assert.Equal(t, "1", threads)
}

func TestOpenCreatesHotTables(t *testing.T) {
	s := openStore(t)

	rows, err := s.DB().Query("SELECT table_name FROM information_schema.tables ORDER BY table_name")
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, []string{"logs", "metric_points", "spans"}, tables)
}

func TestMemoryByTag(t *testing.T) {
	s := openStore(t)
	w, err := store.NewWriter(t.Context(), s)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	_, err = w.Write(t.Context(), sampleBatch(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	require.NoError(t, err)

	mem, err := store.MemoryByTag(t.Context(), s.DB())
	require.NoError(t, err)
	// Freshly committed rows show up under IN_MEMORY_TABLE, not BASE_TABLE,
	// so only check that some tag is accounted.
	assert.NotEmpty(t, mem.ByTag)
	for tag, n := range mem.ByTag {
		assert.Positive(t, n, tag)
	}
	assert.GreaterOrEqual(t, mem.Temp, int64(0))
}

func TestOpenAppliesAllocatorSettings(t *testing.T) {
	s, err := store.Open(t.TempDir(), store.Limits{
		MemoryLimit: "64MB", Threads: 1,
		AllocatorFlushThreshold: "16MB", AllocatorBackgroundThreads: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	var threshold, background string
	err = s.DB().QueryRow(
		"SELECT current_setting('allocator_flush_threshold'), current_setting('allocator_background_threads')::VARCHAR",
	).Scan(&threshold, &background)
	require.NoError(t, err)
	assert.Equal(t, "15.2 MiB", threshold) // 16 * 10^6 bytes
	assert.Equal(t, "true", background)
}

func TestOpenKeepsAllocatorDefaults(t *testing.T) {
	s := openStore(t)

	var threshold, background string
	err := s.DB().QueryRow(
		"SELECT current_setting('allocator_flush_threshold'), current_setting('allocator_background_threads')::VARCHAR",
	).Scan(&threshold, &background)
	require.NoError(t, err)
	assert.Equal(t, "128.0 MiB", threshold)
	assert.Equal(t, "false", background)
}

func TestNewConnectionAfterSpill(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(dir, store.Limits{MemoryLimit: "32MB", Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := t.Context()

	// A sort larger than the memory limit spills to the temp directory.
	held, err := s.Conn(ctx)
	require.NoError(t, err)
	defer held.Close()
	_, err = held.ExecContext(ctx,
		"CREATE TABLE big AS SELECT i, md5(i::VARCHAR) AS s FROM range(3000000) t(i) ORDER BY s")
	require.NoError(t, err)

	// A connection opened after the temp directory was used must still boot.
	fresh, err := s.Conn(ctx)
	require.NoError(t, err)
	defer fresh.Close()
	var tmp string
	require.NoError(t, fresh.QueryRowContext(ctx, "SELECT current_setting('temp_directory')").Scan(&tmp))
	assert.Equal(t, filepath.Join(dir, "tmp"), tmp)
}
