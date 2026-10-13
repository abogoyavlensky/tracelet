package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"slices"
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
	follow := fs.Bool("follow", false, "print the newest page, then new logs as they arrive, until interrupted")
	interval := fs.Duration("interval", 2*time.Second, "with --follow, the time between polls")
	pos, err := parse(fs, &e, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "logs search takes flags only"}
	}
	if *follow {
		if *cursor != "" || rf.from != "" || rf.to != "" {
			return usageError{msg: "--follow cannot be combined with --cursor, --from, or --to; it takes --last or no range"}
		}
		if *interval <= 0 {
			return usageError{msg: "--follow needs a positive --interval"}
		}
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
	if *limit != 0 {
		// Forwarded as given, so the server rejects a bad one.
		q.Set("limit", fmt.Sprint(*limit))
	}
	for _, a := range attrs {
		q.Add("attr", a)
	}

	if *follow {
		return followLogs(ctx, e, q, *interval, stdout, stderr)
	}
	body, err := e.client.GetJSON(ctx, "/api/v1/logs", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	page, err := decodeLogsPage(body)
	if err != nil {
		return err
	}
	if err := writeLogLines(stdout, page.Items, false); err != nil {
		return err
	}
	if page.NextCursor != "" {
		fmt.Fprintf(stderr, "more results: --cursor %s\n", page.NextCursor)
	}
	return nil
}

// logsPage is the part of a logs response the CLI reads. Items stay raw so
// --json can print them exactly as the server sent them.
type logsPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor"`
	Limit      int               `json:"limit"`
	Truncated  bool              `json:"truncated"`
	TailCursor string            `json:"tail_cursor"`
}

func decodeLogsPage(body []byte) (logsPage, error) {
	var page logsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return logsPage{}, fmt.Errorf("decode logs: %w", err)
	}
	return page, nil
}

// followLogs prints the newest page oldest first, then each poll's new logs
// oldest first, like tail -f, until ctx is cancelled, which is a clean end.
// A log that arrives late prints when it arrives, so the stream is in
// arrival order, not strictly in ts order. With --json every log is one
// line (JSON Lines), since a single document cannot stream.
func followLogs(ctx context.Context, e env, q url.Values, interval time.Duration, stdout, stderr io.Writer) error {
	cursor := ""
	for {
		if cursor != "" {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			q.Set("since", cursor)
		}
		body, err := e.client.GetJSON(ctx, "/api/v1/logs", q)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		page, err := decodeLogsPage(body)
		if err != nil {
			return err
		}
		if page.TailCursor == "" {
			return errors.New("the server returned no tail_cursor; it may be too old for --follow")
		}
		cursor = page.TailCursor

		if e.json {
			err = writeJSONLines(stdout, page.Items)
		} else {
			err = writeLogLines(stdout, page.Items, true)
		}
		if err != nil {
			return err
		}
		if q.Has("since") && page.Truncated {
			fmt.Fprintf(stderr, "more than %d new records since the last poll; some were not shown\n", page.Limit)
		}
	}
}

// writeJSONLines prints each log on its own line, oldest first (the server
// sends them newest first).
func writeJSONLines(w io.Writer, items []json.RawMessage) error {
	for _, item := range slices.Backward(items) {
		var line bytes.Buffer
		if err := json.Compact(&line, item); err != nil {
			return fmt.Errorf("decode log: %w", err)
		}
		line.WriteByte('\n')
		if _, err := w.Write(line.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// writeLogLines prints one aligned line per log. The server sends logs
// newest first; oldestFirst reverses them.
func writeLogLines(w io.Writer, items []json.RawMessage, oldestFirst bool) error {
	logs := make([]struct {
		TS             time.Time `json:"ts"`
		Service        string    `json:"service"`
		SeverityNumber int       `json:"severity_number"`
		SeverityText   string    `json:"severity_text"`
		Body           string    `json:"body"`
	}, len(items))
	for i, item := range items {
		if err := json.Unmarshal(item, &logs[i]); err != nil {
			return fmt.Errorf("decode log: %w", err)
		}
	}
	if oldestFirst {
		slices.Reverse(logs)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, l := range logs {
		lvl := l.SeverityText
		if lvl == "" {
			lvl = levelName(l.SeverityNumber)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", l.TS.UTC().Format(tsFormat), lvl, l.Service,
			strings.ReplaceAll(l.Body, "\n", `\n`))
	}
	return tw.Flush()
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
			Spans   int64  `json:"spans"`
			Points  int64  `json:"points"`
		}
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode services: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, s := range list.Items {
		fmt.Fprintf(tw, "%s\t%d logs\t%d spans\t%d points\n", s.Service, s.Logs, s.Spans, s.Points)
	}
	return tw.Flush()
}
