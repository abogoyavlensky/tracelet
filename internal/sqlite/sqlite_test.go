package sqlite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

func TestOpenSetsPragmas(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var journal string
	var busy, fk int
	require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journal))
	require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&busy))
	require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))

	assert.Equal(t, "wal", journal)
	assert.Equal(t, 5000, busy)
	assert.Equal(t, 1, fk)
}

func TestMigrateAppliesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tracelet.sqlite")

	db, err := sqlite.Open(path)
	require.NoError(t, err)
	applied, err := sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	assert.Equal(t, []int{1}, applied)
	require.NoError(t, db.Close())

	db, err = sqlite.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	applied, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	assert.Empty(t, applied)

	version, err := sqlite.Version(t.Context(), db)
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	require.NoError(t, err)
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"files", "projects", "schema_migrations", "tokens"}, tables)
}

func TestForeignKeysEnforced(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO tokens (id, project_id, scope, name, prefix, hash, created_at)
		VALUES ('t1', 'missing', 'ingest', 'x', 'abcdef', x'00', '2026-01-01T00:00:00Z')`)
	require.Error(t, err)
}

func TestOpenPathWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "odd?dir#1%20")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "tracelet.sqlite")

	db, err := sqlite.Open(path)
	require.NoError(t, err)
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	assert.FileExists(t, path)
}
