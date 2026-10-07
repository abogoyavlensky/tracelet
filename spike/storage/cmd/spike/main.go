// Command spike runs the storage spike scenarios and prints JSON reports.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: spike <command> [flags]

Commands:
  version   Print the version
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
