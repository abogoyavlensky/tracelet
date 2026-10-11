package cli_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	traceID      = "4bf92f3577b34da6a3ce929d0e0e4736"
	cycleTraceID = "0af7651916cd43dd8448eb211c80319c"

	servicesJSON = `{"items":[{"service":"api","logs":125,"spans":2},{"service":"worker","logs":126,"spans":1}],` +
		`"range":{"from":"2026-10-10T11:00:00Z","to":"2026-10-10T12:00:00Z"}}`
	// The grandchild is listed before its parent to check nesting does not
	// depend on order.
	traceJSON = `{"trace_id":"` + traceID + `","start":"2026-10-10T11:59:00Z","end":"2026-10-10T11:59:00.2Z",` +
		`"duration_ns":200000000,"services":["api","worker"],"spans":[` +
		`{"ts":"2026-10-10T11:59:00Z","end_ts":"2026-10-10T11:59:00.2Z","duration_ns":200000000,"service":"api",` +
		`"trace_id":"` + traceID + `","span_id":"a1","name":"GET /checkout","kind":2,"status_code":0},` +
		`{"ts":"2026-10-10T11:59:00.03Z","end_ts":"2026-10-10T11:59:00.13Z","duration_ns":100000000,"service":"worker",` +
		`"trace_id":"` + traceID + `","span_id":"c1","parent_span_id":"b1","name":"charge","kind":5,"status_code":2,` +
		`"status_message":"card declined"},` +
		`{"ts":"2026-10-10T11:59:00.01Z","end_ts":"2026-10-10T11:59:00.16Z","duration_ns":150000000,"service":"api",` +
		`"trace_id":"` + traceID + `","span_id":"b1","parent_span_id":"a1","name":"POST /charge","kind":3,"status_code":1}` +
		`],"range":{"from":"2026-10-03T12:00:00Z","to":"2026-10-10T12:00:00Z"},"truncated":false}`
	cycleTraceJSON = `{"trace_id":"` + cycleTraceID + `","start":"2026-10-10T11:59:00Z","end":"2026-10-10T11:59:01Z",` +
		`"duration_ns":1000000000,"services":["api"],"spans":[` +
		`{"ts":"2026-10-10T11:59:00Z","duration_ns":1000,"service":"api","span_id":"x1","parent_span_id":"x2","name":"one","kind":1,"status_code":0},` +
		`{"ts":"2026-10-10T11:59:00.5Z","duration_ns":1000,"service":"api","span_id":"x2","parent_span_id":"x1","name":"two","kind":1,"status_code":0}` +
		`],"range":{"from":"2026-10-03T12:00:00Z","to":"2026-10-10T12:00:00Z"},"truncated":false}`
	spansJSON = `{"items":[` +
		`{"ts":"2026-10-10T11:59:00.5Z","duration_ns":1234567,"service":"api","trace_id":"` + traceID + `","span_id":"a1",` +
		`"name":"GET /checkout","kind":2,"status_code":2},` +
		`{"ts":"2026-10-10T11:58:00Z","duration_ns":5000000,"service":"worker","trace_id":"` + cycleTraceID + `","span_id":"b1",` +
		`"name":"tick","kind":1,"status_code":0}` +
		`],"next_cursor":"def","range":{"from":"2026-10-10T11:00:00Z","to":"2026-10-10T12:00:00Z"},"limit":100,"truncated":true}`
)

func TestTracesGet(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "traces", "get", traceID, "--url", srv.URL, "--project", "shop", "--last", "720h")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{"project": {"shop"}, "last": {"720h"}}, f.last.URL.Query())

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, []string{"trace", traceID, "3", "spans", "2", "services", "start", "2026-10-10T11:59:00.000Z", "duration", "200ms"},
		strings.Fields(lines[0]))
	assert.True(t, strings.HasPrefix(lines[1], "GET /checkout"), lines[1])
	assert.True(t, strings.HasPrefix(lines[2], "  POST /charge"), lines[2])
	assert.True(t, strings.HasPrefix(lines[3], "    charge"), lines[3])
	assert.Equal(t, []string{"GET", "/checkout", "api", "server", "200ms", "unset"}, strings.Fields(lines[1]))
	assert.Equal(t, []string{"POST", "/charge", "api", "client", "150ms", "ok"}, strings.Fields(lines[2]))
	assert.Equal(t, []string{"charge", "worker", "consumer", "100ms", "error", "card", "declined"}, strings.Fields(lines[3]))

	code, out, _ = run(t, "traces", "get", traceID, "--url", srv.URL, "--json")
	require.Equal(t, 0, code)
	assert.Equal(t, traceJSON+"\n", out)
}

func TestTracesGetParentCycle(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)

	code, out, errOut := run(t, "traces", "get", cycleTraceID, "--url", srv.URL)
	require.Equal(t, 0, code, errOut)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3, "every span exactly once")
	assert.True(t, strings.HasPrefix(lines[1], "one"), lines[1])
	assert.True(t, strings.HasPrefix(lines[2], "  two"), lines[2])
}

func TestTracesGetUsage(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)
	for name, args := range map[string][]string{
		"no id":          {"traces", "get", "--url", srv.URL},
		"two ids":        {"traces", "get", traceID, cycleTraceID, "--url", srv.URL},
		"no subcommand":  {"traces"},
		"bad subcommand": {"traces", "list", "--url", srv.URL},
	} {
		code, _, errOut := run(t, args...)
		assert.Equal(t, 2, code, name)
		assert.Contains(t, errOut, "Usage:", name)
	}
}

func TestSpansSearch(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "spans", "search", "--url", srv.URL, "--project", "shop", "--service", "api",
		"--name", "GET /checkout", "--status", "error", "--min-duration", "500ms", "--root", "--trace-id", traceID,
		"--attr", "http.route:/checkout", "--environment", "prod", "--last", "1h", "--limit", "50")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{
		"project": {"shop"}, "service": {"api"}, "name": {"GET /checkout"}, "status": {"error"},
		"min_duration": {"500ms"}, "root": {"true"}, "trace_id": {traceID}, "attr": {"http.route:/checkout"},
		"environment": {"prod"}, "last": {"1h"}, "limit": {"50"},
	}, f.last.URL.Query())

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"2026-10-10T11:59:00.500Z", "1.235ms", "api", "GET", "/checkout", "error", traceID}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"2026-10-10T11:58:00.000Z", "5ms", "worker", "tick", "unset", cycleTraceID}, strings.Fields(lines[1]))
	assert.Contains(t, errOut, "--cursor def")

	run(t, "spans", "search", "--url", srv.URL)
	assert.Empty(t, f.last.URL.Query(), "no filters, no parameters")

	code, out, _ = run(t, "spans", "search", "--url", srv.URL, "--json")
	require.Equal(t, 0, code)
	assert.Equal(t, spansJSON+"\n", out)
}

func TestServices(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "services", "--url", srv.URL, "--last", "24h")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{"last": {"24h"}}, f.last.URL.Query())
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"api", "125", "logs", "2", "spans"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"worker", "126", "logs", "1", "spans"}, strings.Fields(lines[1]))
}
