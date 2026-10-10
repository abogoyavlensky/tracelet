package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the server's configuration, loaded once at startup.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// DataDir holds the databases and telemetry files.
	DataDir string
	// Version is reported by the health endpoint and the CLI.
	Version string
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration

	// DuckDBMemoryLimit is DuckDB's memory_limit, a DuckDB size string.
	DuckDBMemoryLimit string
	// DuckDBThreads is DuckDB's thread count.
	DuckDBThreads int
	// RetentionDays keeps cold telemetry for this many days, per signal.
	RetentionDays int
	// MaxTelemetryBytes caps the cold files' total size, oldest hours going
	// first; zero means no cap.
	MaxTelemetryBytes int64
	// DiskFloorBytes is the free disk below which ingestion is refused.
	DiskFloorBytes int64
	// MaintenanceInterval is how often flush and retention run.
	MaintenanceInterval time.Duration
	// AdminToken, when set, seeds the first admin token instead of a random
	// one. It comes from TRACELET_ADMIN_TOKEN only, never a flag, so it does
	// not show up in the process list.
	AdminToken string
}

// ParseServeConfig builds the configuration from the serve command's flags.
// TRACELET_* environment variables supply the defaults, so a container can
// be configured without flags.
func ParseServeConfig(args []string, version string) (Config, error) {
	cfg := Config{
		Version:         version,
		ShutdownTimeout: 10 * time.Second,
		AdminToken:      os.Getenv("TRACELET_ADMIN_TOKEN"),
	}

	threads, err := envInt("TRACELET_DUCKDB_THREADS", 2)
	if err != nil {
		return Config{}, err
	}
	retention, err := envInt("TRACELET_RETENTION_DAYS", 30)
	if err != nil {
		return Config{}, err
	}
	maxBytes, err := envInt64("TRACELET_MAX_TELEMETRY_BYTES", 0)
	if err != nil {
		return Config{}, err
	}
	diskFloor, err := envInt64("TRACELET_DISK_FLOOR_BYTES", 1_000_000_000)
	if err != nil {
		return Config{}, err
	}
	interval, err := envDuration("TRACELET_MAINTENANCE_INTERVAL", time.Minute)
	if err != nil {
		return Config{}, err
	}

	flags := flag.NewFlagSet("tracelet serve", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", envOr("TRACELET_ADDR", ":8080"), "address to listen on")
	flags.StringVar(&cfg.DataDir, "data-dir", envOr("TRACELET_DATA_DIR", "./data"), "directory for databases and telemetry files")
	flags.StringVar(&cfg.DuckDBMemoryLimit, "duckdb-memory-limit", envOr("TRACELET_DUCKDB_MEMORY_LIMIT", "256MB"), "DuckDB memory limit")
	flags.IntVar(&cfg.DuckDBThreads, "duckdb-threads", threads, "DuckDB threads")
	flags.IntVar(&cfg.RetentionDays, "retention-days", retention, "days to keep telemetry")
	flags.Int64Var(&cfg.MaxTelemetryBytes, "max-telemetry-bytes", maxBytes, "cap on stored telemetry in bytes, oldest first; 0 for no cap")
	flags.Int64Var(&cfg.DiskFloorBytes, "disk-floor-bytes", diskFloor, "free disk in bytes below which ingestion is refused")
	flags.DurationVar(&cfg.MaintenanceInterval, "maintenance-interval", interval, "how often flush and retention run")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.Addr == "" {
		return errors.New("addr is required")
	}
	if cfg.DataDir == "" {
		return errors.New("data-dir is required")
	}
	if cfg.DuckDBMemoryLimit == "" || strings.ContainsAny(cfg.DuckDBMemoryLimit, "'\\") {
		return fmt.Errorf("invalid duckdb-memory-limit %q", cfg.DuckDBMemoryLimit)
	}
	if cfg.DuckDBThreads < 1 {
		return errors.New("duckdb-threads must be at least 1")
	}
	if cfg.RetentionDays < 1 {
		return errors.New("retention-days must be at least 1")
	}
	if cfg.MaxTelemetryBytes < 0 {
		return errors.New("max-telemetry-bytes must not be negative")
	}
	if cfg.DiskFloorBytes < 0 {
		return errors.New("disk-floor-bytes must not be negative")
	}
	if cfg.MaintenanceInterval <= 0 {
		return errors.New("maintenance-interval must be positive")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
