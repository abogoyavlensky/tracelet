package store_test

import (
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
