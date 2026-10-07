package app_test

import (
	"testing"

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := app.ParseServeConfig(tt.args, "dev")
			assert.Error(t, err)
		})
	}
}
