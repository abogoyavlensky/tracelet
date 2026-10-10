package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

// repeated is a flag that may be given several times.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

// rangeFlags are the time range flags of the query commands.
type rangeFlags struct{ project, from, to, last string }

func (rf *rangeFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&rf.project, "project", "", "project slug (required unless the token is for one project)")
	fs.StringVar(&rf.from, "from", "", "start, RFC 3339")
	fs.StringVar(&rf.to, "to", "", "end, RFC 3339")
	fs.StringVar(&rf.last, "last", "", "range ending now, such as 30m or 2h")
}

func (rf rangeFlags) set(q url.Values) {
	for k, v := range map[string]string{"project": rf.project, "from": rf.from, "to": rf.to, "last": rf.last} {
		if v != "" {
			q.Set(k, v)
		}
	}
}

func runLogs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("logs", args)
	if err != nil {
		return err
	}
	if sub != "search" {
		return usageError{msg: fmt.Sprintf("unknown logs subcommand %q", sub)}
	}

	var e env
	fs := newFlags("logs search", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	var attrs repeated
	service := fs.String("service", "", "service name")
	environment := fs.String("environment", "", "deployment environment")
	level := fs.String("level", "", "minimum level: trace, debug, info, warn, error, fatal")
	text := fs.String("q", "", "case-insensitive substring of the body")
	traceID := fs.String("trace-id", "", "trace ID")
	fs.Var(&attrs, "attr", "key:value attribute match; repeatable")
	limit := fs.Int("limit", 0, "page size, up to 1000 (default 100)")
	cursor := fs.String("cursor", "", "next page cursor from a previous search")
	pos, err := parse(fs, &e, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "logs search takes flags only"}
	}

	q := url.Values{}
	rf.set(q)
	for k, v := range map[string]string{
		"service": *service, "environment": *environment, "level": *level, "q": *text,
		"trace_id": *traceID, "cursor": *cursor,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if *limit > 0 {
		q.Set("limit", fmt.Sprint(*limit))
	}
	for _, a := range attrs {
		q.Add("attr", a)
	}

	body, err := e.client.GetJSON(ctx, "/api/v1/logs", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var page struct {
		Items []struct {
			TS             time.Time `json:"ts"`
			Service        string    `json:"service"`
			SeverityNumber int       `json:"severity_number"`
			SeverityText   string    `json:"severity_text"`
			Body           string    `json:"body"`
		}
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode logs: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, l := range page.Items {
		lvl := l.SeverityText
		if lvl == "" {
			lvl = levelName(l.SeverityNumber)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", l.TS.UTC().Format("2006-01-02T15:04:05.000Z"), lvl, l.Service,
			strings.ReplaceAll(l.Body, "\n", `\n`))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if page.NextCursor != "" {
		fmt.Fprintf(stderr, "more results: --cursor %s\n", page.NextCursor)
	}
	return nil
}

// levelName names an OTLP severity number's range.
func levelName(n int) string {
	switch {
	case n >= 21:
		return "FATAL"
	case n >= 17:
		return "ERROR"
	case n >= 13:
		return "WARN"
	case n >= 9:
		return "INFO"
	case n >= 5:
		return "DEBUG"
	case n >= 1:
		return "TRACE"
	default:
		return "-"
	}
}

func runServices(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("services", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "services takes flags only"}
	}
	q := url.Values{}
	rf.set(q)
	body, err := e.client.GetJSON(ctx, "/api/v1/services", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var list struct {
		Items []struct {
			Service string `json:"service"`
			Logs    int64  `json:"logs"`
		}
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode services: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, s := range list.Items {
		fmt.Fprintf(tw, "%s\t%d\n", s.Service, s.Logs)
	}
	return tw.Flush()
}
