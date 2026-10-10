package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

func runProjects(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("projects", args)
	if err != nil {
		return err
	}
	var e env
	switch sub {
	case "list":
		fs := newFlags("projects list", &e, stderr)
		pos, err := parse(fs, &e, rest)
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return usageError{msg: "projects list takes no arguments"}
		}
		body, err := e.client.GetJSON(ctx, "/api/v1/projects", nil)
		if err != nil {
			return err
		}
		if e.json {
			return writeRaw(stdout, body)
		}
		var list struct {
			Items []struct{ Slug, Name string }
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return fmt.Errorf("decode projects: %w", err)
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		for _, p := range list.Items {
			fmt.Fprintf(tw, "%s\t%s\n", p.Slug, p.Name)
		}
		return tw.Flush()

	case "create":
		fs := newFlags("projects create", &e, stderr)
		name := fs.String("name", "", "display name (default: the slug)")
		pos, err := parse(fs, &e, rest)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError{msg: "projects create takes one slug"}
		}
		body, err := e.client.PostJSON(ctx, "/api/v1/projects", map[string]string{"slug": pos[0], "name": *name})
		if err != nil {
			return err
		}
		if e.json {
			return writeRaw(stdout, body)
		}
		fmt.Fprintf(stdout, "created project %s\n", pos[0])
		return nil

	default:
		return usageError{msg: fmt.Sprintf("unknown projects subcommand %q", sub)}
	}
}

// writeRaw prints the server's JSON as it came, with a final newline.
func writeRaw(w io.Writer, body []byte) error {
	if _, err := w.Write(body); err != nil {
		return err
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}
