package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"text/tabwriter"
	"time"
)

// maxDashboardFile matches the server's limit on a request body.
const maxDashboardFile = 1 << 20

func runDashboards(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("dashboards", args)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		return dashboardsList(ctx, rest, stdout, stderr)
	case "export":
		return dashboardsExport(ctx, rest, stdout, stderr)
	case "apply":
		return dashboardsApply(ctx, rest, stdout, stderr)
	case "delete":
		return dashboardsDelete(ctx, rest, stdout, stderr)
	default:
		return usageError{msg: fmt.Sprintf("unknown dashboards subcommand %q", sub)}
	}
}

func projectFlag(fs *flag.FlagSet) *string {
	return fs.String("project", "", "project slug (required unless the token is for one project)")
}

func projectQuery(project string) url.Values {
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	return q
}

func dashboardPath(uid string) string { return "/api/v1/dashboards/" + url.PathEscape(uid) }

func dashboardsList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("dashboards list", &e, stderr)
	project := projectFlag(fs)
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "dashboards list takes flags only"}
	}
	body, err := e.client.GetJSON(ctx, "/api/v1/dashboards", projectQuery(*project))
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var list struct {
		Items []struct {
			UID       string    `json:"uid"`
			Title     string    `json:"title"`
			Panels    int       `json:"panels"`
			Revision  int       `json:"revision"`
			UpdatedAt time.Time `json:"updated_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode dashboards: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "UID\tTITLE\tPANELS\tREVISION\tUPDATED")
	for _, d := range list.Items {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n", d.UID, d.Title, d.Panels, d.Revision, d.UpdatedAt.UTC().Format(tsFormat))
	}
	return tw.Flush()
}

// dashboardsExport prints a dashboard as a file for apply: without its
// revision, so applying it later overwrites rather than conflicts, and
// without timestamps, so the file only changes when the dashboard does.
func dashboardsExport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("dashboards export", &e, stderr)
	project := projectFlag(fs)
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{msg: "dashboards export takes one uid"}
	}
	body, err := e.client.GetJSON(ctx, dashboardPath(pos[0]), projectQuery(*project))
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode dashboard: %w", err)
	}
	for _, k := range []string{"revision", "created_at", "updated_at"} {
		delete(doc, k)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dashboard: %w", err)
	}
	return writeRaw(stdout, out)
}

func dashboardsApply(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("dashboards apply", &e, stderr)
	project := projectFlag(fs)
	file := fs.String("file", "", "the dashboard JSON file, or - for stdin")
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 || *file == "" {
		return usageError{msg: "dashboards apply needs --file"}
	}
	data, err := readDashboardFile(*file)
	if err != nil {
		return err
	}
	var head struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("read %s: not a JSON dashboard: %w", *file, err)
	}
	if head.UID == "" {
		return usageError{msg: "the dashboard file needs a uid, the dashboard's stable identifier"}
	}

	path := dashboardPath(head.UID)
	if q := projectQuery(*project); len(q) > 0 {
		path += "?" + q.Encode()
	}
	body, err := e.client.PutJSON(ctx, path, json.RawMessage(data))
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var d struct {
		Revision int `json:"revision"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("decode dashboard: %w", err)
	}
	fmt.Fprintf(stdout, "applied dashboard %s (revision %d)\n", head.UID, d.Revision)
	return nil
}

func readDashboardFile(name string) ([]byte, error) {
	var r io.Reader = os.Stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, maxDashboardFile+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > maxDashboardFile {
		return nil, fmt.Errorf("read %s: larger than 1 MB", name)
	}
	return data, nil
}

func dashboardsDelete(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("dashboards delete", &e, stderr)
	project := projectFlag(fs)
	yes := fs.Bool("yes", false, "confirm the deletion")
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{msg: "dashboards delete takes one uid"}
	}
	if !*yes {
		return usageError{msg: "dashboards delete cannot be undone; pass --yes to confirm"}
	}
	path := dashboardPath(pos[0])
	if q := projectQuery(*project); len(q) > 0 {
		path += "?" + q.Encode()
	}
	if err := e.client.Delete(ctx, path); err != nil {
		return err
	}
	if e.json {
		return json.NewEncoder(stdout).Encode(map[string]any{"uid": pos[0], "deleted": true})
	}
	fmt.Fprintf(stdout, "deleted dashboard %s\n", pos[0])
	return nil
}
