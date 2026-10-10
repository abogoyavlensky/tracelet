package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"
)

func runTokens(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("tokens", args)
	if err != nil {
		return err
	}
	var e env
	switch sub {
	case "list":
		fs := newFlags("tokens list", &e, stderr)
		if _, err := parse(fs, &e, rest); err != nil {
			return err
		}
		body, err := e.client.GetJSON(ctx, "/api/v1/tokens", nil)
		if err != nil {
			return err
		}
		if e.json {
			return writeRaw(stdout, body)
		}
		var list struct {
			Items []struct {
				ID, Project, Scope, Name, Prefix string
				RevokedAt                        string `json:"revoked_at"`
			}
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return fmt.Errorf("decode tokens: %w", err)
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		for _, t := range list.Items {
			project, state := t.Project, "active"
			if project == "" {
				project = "*"
			}
			if t.RevokedAt != "" {
				state = "revoked"
			}
			fmt.Fprintf(tw, "%s\ttl_%s…\t%s\t%s\t%s\t%s\n", t.ID, t.Prefix, t.Scope, project, state, t.Name)
		}
		return tw.Flush()

	case "create":
		fs := newFlags("tokens create", &e, stderr)
		project := fs.String("project", "", "project slug (required for ingest)")
		scope := fs.String("scope", "", "ingest, read, or admin")
		name := fs.String("name", "", "what the token is for")
		pos, err := parse(fs, &e, rest)
		if err != nil {
			return err
		}
		if len(pos) > 0 || *scope == "" || *name == "" {
			return usageError{msg: "tokens create needs --scope and --name"}
		}
		body, err := e.client.PostJSON(ctx, "/api/v1/tokens", map[string]string{
			"project": *project, "scope": *scope, "name": *name,
		})
		if err != nil {
			return err
		}
		if e.json {
			return writeRaw(stdout, body)
		}
		var created struct{ Token string }
		if err := json.Unmarshal(body, &created); err != nil {
			return fmt.Errorf("decode token: %w", err)
		}
		fmt.Fprintln(stdout, created.Token)
		fmt.Fprintln(stderr, "Save this token now: it will not be shown again.")
		return nil

	case "revoke":
		fs := newFlags("tokens revoke", &e, stderr)
		pos, err := parse(fs, &e, rest)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError{msg: "tokens revoke takes one token ID"}
		}
		if err := e.client.Delete(ctx, "/api/v1/tokens/"+url.PathEscape(pos[0])); err != nil {
			return err
		}
		if e.json {
			// The API answers 204 with no body; give scripts an object to parse.
			return json.NewEncoder(stdout).Encode(map[string]any{"id": pos[0], "revoked": true})
		}
		fmt.Fprintf(stdout, "revoked token %s\n", pos[0])
		return nil

	default:
		return usageError{msg: fmt.Sprintf("unknown tokens subcommand %q", sub)}
	}
}
