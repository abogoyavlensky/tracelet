package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1 // the server or the network failed
	exitUsage = 2
)

const usage = `Usage: tracelet <command> [flags]

Commands:
  serve                                 Run the server
  version                               Print the version
  projects list                         List projects
  projects create <slug> [--name N]     Create a project
  tokens list                           List tokens
  tokens create --scope S --name N [--project P]
                                        Create a token; prints it once
  tokens revoke <id>                    Revoke a token
  logs search [filters]                 Search logs
  services [--project P] [--last D]     List services with logs

Every client command takes --url (default $TRACELET_URL or
http://localhost:8080), --token (default $TRACELET_TOKEN), and --json.
Run "tracelet <command> -h" for a command's flags.
`

// Usage is the top-level help, shared with the server's commands.
func Usage() string { return usage }

// env is the configuration every client command shares.
type env struct {
	client Client
	json   bool
}

// usageError is a bad command line: exit 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// Run runs one client command and returns its exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var err error
	switch cmd, rest := args[0], args[1:]; cmd {
	case "projects":
		err = runProjects(ctx, rest, stdout, stderr)
	case "tokens":
		err = runTokens(ctx, rest, stdout, stderr)
	case "logs":
		err = runLogs(ctx, rest, stdout, stderr)
	case "services":
		err = runServices(ctx, rest, stdout, stderr)
	default:
		err = usageError{msg: fmt.Sprintf("unknown command %q", cmd)}
	}

	if uerr, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintf(stderr, "%s\n\n%s", uerr.msg, usage)
		return exitUsage
	}
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitError
	}
	return exitOK
}

// newFlags returns a flag set with the shared client flags bound to e.
func newFlags(name string, e *env, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("tracelet "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	baseURL := os.Getenv("TRACELET_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	fs.StringVar(&e.client.BaseURL, "url", baseURL, "server URL")
	fs.StringVar(&e.client.Token, "token", os.Getenv("TRACELET_TOKEN"), "API token")
	fs.BoolVar(&e.json, "json", false, "print the server's JSON")
	e.client.HTTP = &http.Client{Timeout: time.Minute}
	return fs
}

// parse parses flags that may come before, between, or after positional
// arguments, and returns the positionals. It also requires a token.
func parse(fs *flag.FlagSet, e *env, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usageError{msg: err.Error()}
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if strings.TrimSpace(e.client.Token) == "" {
		return nil, usageError{msg: "a token is required: pass --token or set TRACELET_TOKEN"}
	}
	return positional, nil
}

// subcommand splits "projects list ..." into "list" and the rest.
func subcommand(group string, args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, usageError{msg: "tracelet " + group + " needs a subcommand"}
	}
	return args[0], args[1:], nil
}
