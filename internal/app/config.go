package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
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
}

// ParseServeConfig builds the configuration from the serve command's flags.
// TRACELET_* environment variables supply the defaults, so a container can
// be configured without flags.
func ParseServeConfig(args []string, version string) (Config, error) {
	cfg := Config{
		Version:         version,
		ShutdownTimeout: 10 * time.Second,
	}

	flags := flag.NewFlagSet("tracelet serve", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", envOr("TRACELET_ADDR", ":8080"), "address to listen on")
	flags.StringVar(&cfg.DataDir, "data-dir", envOr("TRACELET_DATA_DIR", "./data"), "directory for databases and telemetry files")

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
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
