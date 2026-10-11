package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

// span is the part of the API's span object the CLI prints.
type span struct {
	TS            time.Time `json:"ts"`
	DurationNS    int64     `json:"duration_ns"`
	Service       string    `json:"service"`
	TraceID       string    `json:"trace_id"`
	SpanID        string    `json:"span_id"`
	ParentSpanID  string    `json:"parent_span_id"`
	Name          string    `json:"name"`
	Kind          int       `json:"kind"`
	StatusCode    int       `json:"status_code"`
	StatusMessage string    `json:"status_message"`
}

const tsFormat = "2006-01-02T15:04:05.000Z"

func runTraces(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("traces", args)
	if err != nil {
		return err
	}
	if sub != "get" {
		return usageError{msg: fmt.Sprintf("unknown traces subcommand %q", sub)}
	}

	var e env
	fs := newFlags("traces get", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	pos, err := parse(fs, &e, rest)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{msg: "traces get takes one trace ID"}
	}

	q := url.Values{}
	rf.set(q)
	body, err := e.client.GetJSON(ctx, "/api/v1/traces/"+url.PathEscape(pos[0]), q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var tr struct {
		TraceID    string    `json:"trace_id"`
		Start      time.Time `json:"start"`
		DurationNS int64     `json:"duration_ns"`
		Services   []string  `json:"services"`
		Spans      []span    `json:"spans"`
		Truncated  bool      `json:"truncated"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("decode trace: %w", err)
	}

	fmt.Fprintf(stdout, "trace %s  %d spans  %d services  start %s  duration %s\n", tr.TraceID, len(tr.Spans),
		len(tr.Services), tr.Start.UTC().Format(tsFormat), duration(tr.DurationNS))
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, n := range tree(tr.Spans) {
		s := n.span
		status := statusName(s.StatusCode)
		if s.StatusMessage != "" {
			status += " " + s.StatusMessage
		}
		fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\t%s\n", strings.Repeat("  ", n.depth), s.Name, s.Service,
			kindName(s.Kind), duration(s.DurationNS), status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if tr.Truncated {
		fmt.Fprintf(stderr, "the trace has more spans than one response holds; showing the first %d\n", len(tr.Spans))
	}
	return nil
}

type node struct {
	span  span
	depth int
}

// tree orders spans depth first for printing: roots (no parent, or a parent
// not in the trace) and every span's children in the order given, which is
// oldest first. Spans no root reaches, such as a parent cycle, are walked
// afterwards as roots, so every span appears exactly once.
func tree(spans []span) []node {
	ids := map[string]bool{}
	for _, s := range spans {
		ids[s.SpanID] = true
	}
	children := map[string][]int{}
	var roots []int
	for i, s := range spans {
		if s.ParentSpanID == "" || !ids[s.ParentSpanID] {
			roots = append(roots, i)
		} else {
			children[s.ParentSpanID] = append(children[s.ParentSpanID], i)
		}
	}

	var out []node
	visited := make([]bool, len(spans))
	var walk func(i, depth int)
	walk = func(i, depth int) {
		if visited[i] {
			return
		}
		visited[i] = true
		out = append(out, node{spans[i], depth})
		for _, c := range children[spans[i].SpanID] {
			walk(c, depth+1)
		}
	}
	for _, i := range roots {
		walk(i, 0)
	}
	for i := range spans {
		walk(i, 0)
	}
	return out
}

func runSpans(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("spans", args)
	if err != nil {
		return err
	}
	if sub != "search" {
		return usageError{msg: fmt.Sprintf("unknown spans subcommand %q", sub)}
	}

	var e env
	fs := newFlags("spans search", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	var attrs repeated
	service := fs.String("service", "", "service name")
	environment := fs.String("environment", "", "deployment environment")
	name := fs.String("name", "", "exact span name")
	status := fs.String("status", "", "status: unset, ok, error")
	minDuration := fs.String("min-duration", "", "minimum duration, such as 500ms")
	root := fs.Bool("root", false, "only root spans, one per trace")
	traceID := fs.String("trace-id", "", "trace ID")
	fs.Var(&attrs, "attr", "key:value attribute match; repeatable")
	limit := fs.Int("limit", 0, "page size, up to 1000 (default 100)")
	cursor := fs.String("cursor", "", "next page cursor from a previous search")
	pos, err := parse(fs, &e, rest)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "spans search takes flags only"}
	}

	q := url.Values{}
	rf.set(q)
	for k, v := range map[string]string{
		"service": *service, "environment": *environment, "name": *name, "status": *status,
		"min_duration": *minDuration, "trace_id": *traceID, "cursor": *cursor,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if *root {
		q.Set("root", "true")
	}
	if *limit != 0 {
		q.Set("limit", fmt.Sprint(*limit))
	}
	for _, a := range attrs {
		q.Add("attr", a)
	}

	body, err := e.client.GetJSON(ctx, "/api/v1/spans", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var page struct {
		Items      []span `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode spans: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, s := range page.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.TS.UTC().Format(tsFormat), duration(s.DurationNS),
			s.Service, s.Name, statusName(s.StatusCode), s.TraceID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if page.NextCursor != "" {
		fmt.Fprintf(stderr, "more results: --cursor %s\n", page.NextCursor)
	}
	return nil
}

// duration prints nanoseconds rounded to microseconds.
func duration(ns int64) string {
	return time.Duration(ns).Round(time.Microsecond).String()
}

// kindName names an OTLP span kind.
func kindName(k int) string {
	switch k {
	case 1:
		return "internal"
	case 2:
		return "server"
	case 3:
		return "client"
	case 4:
		return "producer"
	case 5:
		return "consumer"
	default:
		return "unspecified"
	}
}

// statusName names an OTLP status code.
func statusName(c int) string {
	switch c {
	case 1:
		return "ok"
	case 2:
		return "error"
	default:
		return "unset"
	}
}
