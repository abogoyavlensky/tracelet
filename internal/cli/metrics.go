package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// metricKind is the part of the API's metric kind the CLI prints. The CLI
// build does not link internal/query, so the shapes are declared here.
type metricKind struct {
	Type      string `json:"type"`
	Monotonic bool   `json:"monotonic"`
	Unit      string `json:"unit"`
}

// label names a kind as instruments do: counter, updown, gauge, histogram.
func (k metricKind) label() string {
	switch {
	case k.Type == "sum" && k.Monotonic:
		return "counter"
	case k.Type == "sum":
		return "updown"
	default:
		return k.Type
	}
}

func runMetrics(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	sub, rest, err := subcommand("metrics", args)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		return metricsList(ctx, rest, stdout, stderr)
	case "attributes":
		return metricsAttributes(ctx, rest, stdout, stderr)
	case "query":
		return metricsQuery(ctx, rest, stdout, stderr)
	default:
		return usageError{msg: fmt.Sprintf("unknown metrics subcommand %q", sub)}
	}
}

// setNonEmpty sets each non-empty value.
func setNonEmpty(q url.Values, values map[string]string) {
	for k, v := range values {
		if v != "" {
			q.Set(k, v)
		}
	}
}

func metricsList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("metrics list", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	service := fs.String("service", "", "service name")
	environment := fs.String("environment", "", "deployment environment")
	text := fs.String("q", "", "case-insensitive substring of the name")
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError{msg: "metrics list takes flags only"}
	}
	q := url.Values{}
	rf.set(q)
	setNonEmpty(q, map[string]string{"service": *service, "environment": *environment, "q": *text})

	body, err := e.client.GetJSON(ctx, "/api/v1/metrics", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var list struct {
		Items []struct {
			Name string `json:"name"`
			metricKind
			Services []string  `json:"services"`
			Series   int64     `json:"series"`
			LastTS   time.Time `json:"last_ts"`
		} `json:"items"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return fmt.Errorf("decode metrics: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tUNIT\tSERIES\tSERVICES\tLAST")
	for _, m := range list.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", m.Name, m.label(), orDash(m.Unit), m.Series,
			strings.Join(m.Services, ","), m.LastTS.UTC().Format(tsFormat))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if list.Truncated {
		fmt.Fprintf(stderr, "more metrics than one response holds; showing the first %d, narrow with --q or --service\n", len(list.Items))
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// maxValuesWidth bounds the VALUES column of metrics attributes.
const maxValuesWidth = 60

func metricsAttributes(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("metrics attributes", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	service := fs.String("service", "", "service name")
	environment := fs.String("environment", "", "deployment environment")
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{msg: "metrics attributes takes one metric name"}
	}
	q := url.Values{"name": {pos[0]}}
	rf.set(q)
	setNonEmpty(q, map[string]string{"service": *service, "environment": *environment})

	body, err := e.client.GetJSON(ctx, "/api/v1/metrics/attributes", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var attrs struct {
		Items []struct {
			Key      string   `json:"key"`
			Distinct int64    `json:"distinct"`
			Values   []string `json:"values"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &attrs); err != nil {
		return fmt.Errorf("decode attributes: %w", err)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tDISTINCT\tVALUES")
	for _, a := range attrs.Items {
		values := strings.Join(a.Values, ",")
		if len(values) > maxValuesWidth {
			values = values[:maxValuesWidth-3] + "..."
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\n", a.Key, a.Distinct, values)
	}
	return tw.Flush()
}

func metricsQuery(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var e env
	fs := newFlags("metrics query", &e, stderr)
	var rf rangeFlags
	rf.bind(fs)
	var attrs repeated
	aggregate := fs.String("aggregate", "", "rate or increase (counters); avg, min, max, sum (gauges); pNN, rate, avg (histograms)")
	groupBy := fs.String("group-by", "", "service, environment, version, or a point attribute key")
	step := fs.String("step", "", "step, such as 1m (default: about 200 steps over the range)")
	service := fs.String("service", "", "service name")
	environment := fs.String("environment", "", "deployment environment")
	fs.Var(&attrs, "attr", "key:value attribute match; repeatable")
	pos, err := parse(fs, &e, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{msg: "metrics query takes one metric name"}
	}
	q := url.Values{"name": {pos[0]}}
	rf.set(q)
	setNonEmpty(q, map[string]string{
		"aggregate": *aggregate, "group_by": *groupBy, "step": *step,
		"service": *service, "environment": *environment,
	})
	for _, a := range attrs {
		q.Add("attr", a)
	}

	body, err := e.client.GetJSON(ctx, "/api/v1/metrics/query", q)
	if err != nil {
		return err
	}
	if e.json {
		return writeRaw(stdout, body)
	}
	var res struct {
		Name string `json:"name"`
		metricKind
		Aggregate   string      `json:"aggregate"`
		StepSeconds int64       `json:"step_seconds"`
		Timestamps  []time.Time `json:"timestamps"`
		Series      []struct {
			Group              map[string]string `json:"group"`
			Values             []*float64        `json:"values"`
			IncompatibleSeries int               `json:"incompatible_series"`
		} `json:"series"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("decode metric: %w", err)
	}

	fmt.Fprintf(stdout, "%s  %s  %s  unit %s  step %s\n", res.Name, res.label(), res.Aggregate, orDash(res.Unit),
		time.Duration(res.StepSeconds)*time.Second)
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	header := []string{"TIME"}
	incompatible := 0
	for _, s := range res.Series {
		header = append(header, groupLabel(s.Group))
		incompatible += s.IncompatibleSeries
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for i, ts := range res.Timestamps {
		row := []string{ts.UTC().Format(tsFormat)}
		for _, s := range res.Series {
			row = append(row, formatValue(s.Values, i))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if res.Truncated {
		fmt.Fprintf(stderr, "more groups than one response holds; showing the %d largest\n", len(res.Series))
	}
	if incompatible > 0 {
		fmt.Fprintf(stderr, "%d series with different bucket bounds are not included\n", incompatible)
	}
	return nil
}

// groupLabel joins a series' group values, or "value" when ungrouped.
func groupLabel(group map[string]string) string {
	if len(group) == 0 {
		return "value"
	}
	var parts []string
	for _, v := range group {
		if v == "" {
			v = "(none)"
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, ",")
}

func formatValue(values []*float64, i int) string {
	if i >= len(values) || values[i] == nil {
		return "-"
	}
	return strconv.FormatFloat(*values[i], 'g', 6, 64)
}
