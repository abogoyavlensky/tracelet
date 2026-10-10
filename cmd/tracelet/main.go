// Command tracelet is the Tracelet server and its CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/abogoyavlensky/tracelet/internal/app"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: tracelet <command> [flags]

Commands:
  serve     Run the server
  version   Print the version

Run "tracelet <command> -h" for the command's flags.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run dispatches the command so that deferred cleanup runs before exit.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return errors.New("missing command")
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "version":
		fmt.Fprintln(stdout, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := app.ParseServeConfig(args, version)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}

	return a.Run(ctx)
}
