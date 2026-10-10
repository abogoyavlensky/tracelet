package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/app"
)

func TestParseServeConfigDefaults(t *testing.T) {
	t.Setenv("TRACELET_ADDR", "")
	t.Setenv("TRACELET_DATA_DIR", "")

	cfg, err := app.ParseServeConfig(nil, "1.2.3")
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, "./data", cfg.DataDir)
	assert.Equal(t, "1.2.3", cfg.Version)
	assert.Positive(t, cfg.ShutdownTimeout)
	assert.Equal(t, "256MB", cfg.DuckDBMemoryLimit)
	assert.Equal(t, 2, cfg.DuckDBThreads)
	assert.Equal(t, 30, cfg.RetentionDays)
	assert.Zero(t, cfg.MaxTelemetryBytes)
	assert.Equal(t, int64(1_000_000_000), cfg.DiskFloorBytes)
	assert.Equal(t, time.Minute, cfg.MaintenanceInterval)
	assert.Empty(t, cfg.AdminToken)
}

func TestParseServeConfigStorageFromEnv(t *testing.T) {
	t.Setenv("TRACELET_DUCKDB_MEMORY_LIMIT", "1GB")
	t.Setenv("TRACELET_DUCKDB_THREADS", "4")
	t.Setenv("TRACELET_RETENTION_DAYS", "7")
	t.Setenv("TRACELET_MAX_TELEMETRY_BYTES", "5000000000")
	t.Setenv("TRACELET_DISK_FLOOR_BYTES", "0")
	t.Setenv("TRACELET_MAINTENANCE_INTERVAL", "30s")
	t.Setenv("TRACELET_ADMIN_TOKEN", "tl_seed")

	cfg, err := app.ParseServeConfig([]string{"--retention-days", "3"}, "dev")
	require.NoError(t, err)

	assert.Equal(t, "1GB", cfg.DuckDBMemoryLimit)
	assert.Equal(t, 4, cfg.DuckDBThreads)
	assert.Equal(t, 3, cfg.RetentionDays, "flags override env")
	assert.Equal(t, int64(5_000_000_000), cfg.MaxTelemetryBytes)
	assert.Zero(t, cfg.DiskFloorBytes)
	assert.Equal(t, 30*time.Second, cfg.MaintenanceInterval)
	assert.Equal(t, "tl_seed", cfg.AdminToken)
}

func TestParseServeConfigRejectsBadEnv(t *testing.T) {
	t.Setenv("TRACELET_DUCKDB_THREADS", "many")
	_, err := app.ParseServeConfig(nil, "dev")
	assert.ErrorContains(t, err, "TRACELET_DUCKDB_THREADS")
}

func TestParseServeConfigFlagsOverrideEnv(t *testing.T) {
	t.Setenv("TRACELET_ADDR", ":9000")
	t.Setenv("TRACELET_DATA_DIR", "/var/lib/tracelet")

	cfg, err := app.ParseServeConfig([]string{"--addr", "127.0.0.1:1"}, "dev")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:1", cfg.Addr)
	assert.Equal(t, "/var/lib/tracelet", cfg.DataDir)
}

func TestParseServeConfigRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"positional argument", []string{"extra"}},
		{"unknown flag", []string{"--nope"}},
		{"empty data dir", []string{"--data-dir", ""}},
		{"zero retention", []string{"--retention-days", "0"}},
		{"negative max bytes", []string{"--max-telemetry-bytes", "-1"}},
		{"negative disk floor", []string{"--disk-floor-bytes", "-1"}},
		{"zero threads", []string{"--duckdb-threads", "0"}},
		{"quoted memory limit", []string{"--duckdb-memory-limit", "1GB'; DROP"}},
		{"zero interval", []string{"--maintenance-interval", "0s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := app.ParseServeConfig(tt.args, "dev")
			assert.Error(t, err)
		})
	}
}
