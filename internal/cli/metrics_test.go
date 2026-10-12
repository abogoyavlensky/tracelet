package cli_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	metricsJSON = `{"items":[` +
		`{"name":"http.server.request.duration","type":"histogram","monotonic":false,"unit":"s","services":["api","worker"],` +
		`"series":12,"points":720,"last_ts":"2026-10-10T11:59:00Z"},` +
		`{"name":"orders.created","type":"sum","monotonic":true,"unit":"{order}","services":["api"],` +
		`"series":2,"points":120,"last_ts":"2026-10-10T11:58:00Z"},` +
		`{"name":"connections.active","type":"sum","monotonic":false,"services":["api"],` +
		`"series":1,"points":60,"last_ts":"2026-10-10T11:58:00Z"}` +
		`],"range":{"from":"2026-10-10T11:00:00Z","to":"2026-10-10T12:00:00Z"},"truncated":true}`
	attributesJSON = `{"name":"orders.created","items":[` +
		`{"key":"plan","distinct":2,"values":["free","pro"]},` +
		`{"key":"user.id","distinct":500,"values":["u000000001","u000000002","u000000003","u000000004","u000000005","u000000006","u000000007"]}` +
		`],"range":{"from":"2026-10-10T11:00:00Z","to":"2026-10-10T12:00:00Z"}}`
	metricQueryJSON = `{"name":"http.server.request.duration","type":"histogram","monotonic":false,"unit":"s",` +
		`"aggregate":"p95","group_by":"http.route","step_seconds":60,` +
		`"range":{"from":"2026-10-10T11:58:00Z","to":"2026-10-10T12:00:00Z"},` +
		`"timestamps":["2026-10-10T11:58:00Z","2026-10-10T11:59:00Z"],"series":[` +
		`{"group":{"http.route":"/cart"},"values":[0.123456789,null],"incompatible_series":2},` +
		`{"group":{"http.route":""},"values":[1.5,2],"incompatible_series":0}` +
		`],"truncated":true}`
)

func TestMetricsList(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "metrics", "list", "--url", srv.URL, "--project", "shop", "--service", "api", "--q", "orders", "--last", "6h")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{"project": {"shop"}, "service": {"api"}, "q": {"orders"}, "last": {"6h"}}, f.last.URL.Query())

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, []string{"NAME", "TYPE", "UNIT", "SERIES", "SERVICES", "LAST"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"http.server.request.duration", "histogram", "s", "12", "api,worker", "2026-10-10T11:59:00.000Z"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"orders.created", "counter", "{order}", "2", "api", "2026-10-10T11:58:00.000Z"}, strings.Fields(lines[2]))
	assert.Equal(t, []string{"connections.active", "updown", "-", "1", "api", "2026-10-10T11:58:00.000Z"}, strings.Fields(lines[3]))
	assert.Contains(t, errOut, "narrow with --q")

	code, out, _ = run(t, "metrics", "list", "--url", srv.URL, "--json")
	require.Equal(t, 0, code)
	assert.Equal(t, metricsJSON+"\n", out)
}

func TestMetricsAttributes(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "metrics", "attributes", "orders.created", "--url", srv.URL, "--environment", "prod")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{"name": {"orders.created"}, "environment": {"prod"}}, f.last.URL.Query())

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"KEY", "DISTINCT", "VALUES"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"plan", "2", "free,pro"}, strings.Fields(lines[1]))
	values := strings.Fields(lines[2])[2]
	assert.Len(t, values, 60, "long value lists are cut")
	assert.True(t, strings.HasSuffix(values, "..."))
}

func TestMetricsQuery(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "metrics", "query", "http.server.request.duration", "--url", srv.URL,
		"--aggregate", "p95", "--group-by", "http.route", "--step", "1m", "--service", "api",
		"--environment", "prod", "--attr", "http.method:GET", "--attr", "region:eu", "--from", "2026-10-10T11:58:00Z")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{
		"name": {"http.server.request.duration"}, "aggregate": {"p95"}, "group_by": {"http.route"}, "step": {"1m"},
		"service": {"api"}, "environment": {"prod"}, "attr": {"http.method:GET", "region:eu"},
		"from": {"2026-10-10T11:58:00Z"},
	}, f.last.URL.Query())

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, []string{"http.server.request.duration", "histogram", "p95", "unit", "s", "step", "1m0s"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"TIME", "/cart", "(none)"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"2026-10-10T11:58:00.000Z", "0.123457", "1.5"}, strings.Fields(lines[2]))
	assert.Equal(t, []string{"2026-10-10T11:59:00.000Z", "-", "2"}, strings.Fields(lines[3]))
	assert.Contains(t, errOut, "showing the 2 largest")
	assert.Contains(t, errOut, "2 series with different bucket bounds")
}

func TestMetricsUsage(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)
	for _, args := range [][]string{
		{"metrics"},
		{"metrics", "graph"},
		{"metrics", "query"},
		{"metrics", "query", "a", "b"},
		{"metrics", "attributes"},
		{"metrics", "list", "extra"},
	} {
		code, _, errOut := run(t, append(args, "--url", srv.URL)...)
		assert.Equal(t, 2, code, "%v: %s", args, errOut)
	}
}
