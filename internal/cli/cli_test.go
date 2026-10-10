package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/cli"
)

// fakeAPI answers like the server and records the last request.
type fakeAPI struct {
	mu   sync.Mutex
	last *http.Request
	body string
}

const (
	projectsJSON = `{"items":[{"id":"a1","slug":"shop","name":"Shop","created_at":"2026-10-10T12:00:00Z"},` +
		`{"id":"b2","slug":"blog","name":"Blog","created_at":"2026-10-10T12:00:00Z"}]}`
	logsJSON = `{"items":[{"ts":"2026-10-10T11:59:00.5Z","service":"api","severity_number":17,` +
		`"severity_text":"ERROR","body":"payment failed"},{"ts":"2026-10-10T11:58:00Z","service":"worker",` +
		`"severity_number":21,"body":"crash"}],"next_cursor":"abc","range":{"from":"2026-10-10T11:30:00Z",` +
		`"to":"2026-10-10T12:00:00Z"},"limit":100,"truncated":true}`
)

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	f := &fakeAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.last = r
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		f.body = buf.String()
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tl_good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid or revoked token"}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/projects":
			_, _ = w.Write([]byte(projectsJSON))
		case "POST /api/v1/tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"t1","scope":"ingest","token":"tl_0123456789abcdef0123456789abcdef01234567"}`))
		case "DELETE /api/v1/tokens/t1":
			w.WriteHeader(http.StatusNoContent)
		case "GET /api/v1/logs":
			_, _ = w.Write([]byte(logsJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestProjectsList(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)

	code, out, _ := run(t, "projects", "list", "--url", srv.URL, "--json")
	require.Equal(t, 0, code)
	assert.Equal(t, projectsJSON+"\n", out, "--json prints the server's JSON verbatim")

	code, out, _ = run(t, "projects", "list", "--url", srv.URL)
	require.Equal(t, 0, code)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "shop")
	assert.Contains(t, lines[0], "Shop")
}

func TestTokensCreate(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "tokens", "create", "--url", srv.URL, "--project", "shop", "--scope", "ingest", "--name", "x")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "tl_0123456789abcdef0123456789abcdef01234567\n", out)
	assert.Contains(t, errOut, "will not be shown again")
	assert.JSONEq(t, `{"project":"shop","scope":"ingest","name":"x"}`, f.body)
}

func TestLogsSearch(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "logs", "search", "--url", srv.URL, "--token", "tl_good",
		"--project", "shop", "--level", "error", "--last", "30m", "--attr", "http.route:/pay", "--attr", "k:v")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{
		"project": {"shop"}, "level": {"error"}, "last": {"30m"}, "attr": {"http.route:/pay", "k:v"},
	}, f.last.URL.Query())

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"2026-10-10T11:59:00.500Z", "ERROR", "api", "payment", "failed"}, strings.Fields(lines[0]))
	assert.Equal(t, []string{"2026-10-10T11:58:00.000Z", "FATAL", "worker", "crash"}, strings.Fields(lines[1]),
		"the level falls back to the severity number")
	assert.Contains(t, errOut, "--cursor abc")

	code, out, _ = run(t, "logs", "search", "--url", srv.URL, "--token", "tl_good", "--json")
	require.Equal(t, 0, code)
	assert.Equal(t, logsJSON+"\n", out)
}

func TestServerErrorExitsOne(t *testing.T) {
	_, srv := newFakeAPI(t)
	code, out, errOut := run(t, "projects", "list", "--url", srv.URL, "--token", "tl_bad")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "invalid or revoked token")
}

func TestNetworkErrorExitsOne(t *testing.T) {
	code, _, errOut := run(t, "projects", "list", "--url", "http://127.0.0.1:1", "--token", "tl_good")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "error:")
}

func TestUsageErrorsExitTwo(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "")
	_, srv := newFakeAPI(t)
	for name, args := range map[string][]string{
		"missing token":        {"projects", "list", "--url", srv.URL},
		"no subcommand":        {"projects"},
		"unknown command":      {"frobnicate"},
		"unknown flag":         {"logs", "search", "--nope"},
		"create without scope": {"tokens", "create", "--token", "tl_good", "--name", "x"},
		"nothing":              {},
	} {
		code, _, errOut := run(t, args...)
		assert.Equal(t, 2, code, name)
		assert.Contains(t, errOut, "Usage:", name)
	}
}

func TestPositionalBeforeFlags(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)
	// The fake 404s on POST /api/v1/projects; the request is what matters.
	code, _, _ := run(t, "projects", "create", "shop", "--name", "Shop", "--url", srv.URL)
	assert.Equal(t, 1, code)
	assert.JSONEq(t, `{"slug":"shop","name":"Shop"}`, f.body)
}

func TestTokensRevokeJSON(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)
	code, out, errOut := run(t, "tokens", "revoke", "t1", "--url", srv.URL, "--json")
	require.Equal(t, 0, code, errOut)
	assert.JSONEq(t, `{"id":"t1","revoked":true}`, out)
}

func TestLogsSearchForwardsNegativeLimit(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)
	run(t, "logs", "search", "--url", srv.URL, "--limit", "-1")
	assert.Equal(t, "-1", f.last.URL.Query().Get("limit"))
}
