package httpapi_test

import (
	"crypto/rand"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/duckdb"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
	"github.com/abogoyavlensky/tracelet/internal/manifest"
	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

var searchNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// newSearchHandler serves the API over a real store holding three logs of
// project p1 (the fake read token's project) and one of a project "other".
func newSearchHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	store, err := duckdb.Open(dir, duckdb.Limits{MemoryLimit: "256MB", Threads: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	db, err := sqlite.Open(filepath.Join(dir, "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	w, err := duckdb.NewWriter(t.Context(), store, time.Time{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	projects := project.NewService(sqlite.NewProjectStore(db), time.Now, rand.Reader)
	other, err := projects.CreateProject(t.Context(), "other", "Other")
	require.NoError(t, err)

	ago := func(m int) time.Time { return searchNow.Add(-time.Duration(m) * time.Minute) }
	_, err = w.Write(t.Context(), telemetry.Batch{Logs: []telemetry.Log{
		{TS: ago(50), Project: "p1", Service: "api", SeverityNumber: 17, Body: "error one"},
		{TS: ago(40), Project: "p1", Service: "worker", SeverityNumber: 9, Body: "info two"},
		{TS: ago(10), Project: "p1", Service: "api", SeverityNumber: 17, Body: "error three",
			Attributes: `{"http.route":"/pay"}`},
		{TS: ago(5), Project: string(other.ID), Service: "web", Body: "other project"},
	}})
	require.NoError(t, err)

	return httpapi.NewHandler(httpapi.Deps{
		Auth: newFakeAuth(), Projects: projects, Logger: discardLogger(),
		Queries: query.Opener{Store: store, Manifest: manifest.New(db), Gate: query.NewGate(4), DataDir: dir},
		Now:     func() time.Time { return searchNow },
	})
}

type pageBody struct {
	Items []struct {
		Body       string         `json:"body"`
		Service    string         `json:"service"`
		Attributes map[string]any `json:"attributes"`
	} `json:"items"`
	NextCursor string      `json:"next_cursor"`
	Range      query.Range `json:"range"`
	Limit      int         `json:"limit"`
	Truncated  bool        `json:"truncated"`
}

func (p pageBody) bodies() []string {
	out := []string{}
	for _, it := range p.Items {
		out = append(out, it.Body)
	}
	return out
}

func getLogs(t *testing.T, h http.Handler, token string, q url.Values) (int, pageBody, string) {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/logs?"+q.Encode(), token, "")
	var page pageBody
	if rec.Code == http.StatusOK {
		page = decode[pageBody](t, rec)
	}
	return rec.Code, page, rec.Body.String()
}

func TestLogsEndpoint(t *testing.T) {
	h := newSearchHandler(t)

	code, page, body := getLogs(t, h, readToken, url.Values{})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, []string{"error three", "info two", "error one"}, page.bodies(), "default range is the last hour")
	assert.Equal(t, query.Range{From: searchNow.Add(-time.Hour), To: searchNow}, page.Range)
	assert.Equal(t, map[string]any{"http.route": "/pay"}, page.Items[0].Attributes)

	_, page, _ = getLogs(t, h, readToken, url.Values{"level": {"error"}, "service": {"api"}, "attr": {"http.route:/pay"}})
	assert.Equal(t, []string{"error three"}, page.bodies())

	_, page, _ = getLogs(t, h, readToken, url.Values{"last": {"30m"}})
	assert.Equal(t, []string{"error three"}, page.bodies())

	_, page, _ = getLogs(t, h, readToken, url.Values{"q": {"TWO"}, "project": {"shop"}})
	assert.Equal(t, []string{"info two"}, page.bodies(), "a project token may name its own project")
}

func TestLogsEndpointPaging(t *testing.T) {
	h := newSearchHandler(t)
	_, page, _ := getLogs(t, h, readToken, url.Values{"limit": {"2"}})
	require.True(t, page.Truncated)
	got := page.bodies()

	code, page, body := getLogs(t, h, readToken, url.Values{"limit": {"2"}, "cursor": {page.NextCursor}})
	require.Equal(t, http.StatusOK, code, body)
	got = append(got, page.bodies()...)
	assert.Empty(t, page.NextCursor)
	assert.Equal(t, []string{"error three", "info two", "error one"}, got)
}

func TestLogsEndpointProjects(t *testing.T) {
	h := newSearchHandler(t)

	code, _, _ := getLogs(t, h, readToken, url.Values{"project": {"other"}})
	assert.Equal(t, http.StatusForbidden, code, "a project token cannot read another project")

	code, _, _ = getLogs(t, h, readAll, url.Values{})
	assert.Equal(t, http.StatusBadRequest, code, "an all-projects token must name one")

	code, page, body := getLogs(t, h, readAll, url.Values{"project": {"other"}})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, []string{"other project"}, page.bodies())

	code, _, _ = getLogs(t, h, readAll, url.Values{"project": {"missing"}})
	assert.Equal(t, http.StatusNotFound, code)

	code, _, _ = getLogs(t, h, ingestToken, url.Values{})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestLogsEndpointBadParameters(t *testing.T) {
	h := newSearchHandler(t)
	for name, q := range map[string]url.Values{
		"cursor with last": {"cursor": {"abc"}, "last": {"1h"}},
		"cursor with from": {"cursor": {"abc"}, "from": {"2026-10-10T11:00:00Z"}},
		"last with from":   {"last": {"1h"}, "from": {"2026-10-10T11:00:00Z"}},
		"bad from":         {"from": {"yesterday"}},
		"bad last":         {"last": {"-5m"}},
		"bad level":        {"level": {"loud"}},
		"bad limit":        {"limit": {"many"}},
		"limit too big":    {"limit": {"1001"}},
		"bad attr":         {"attr": {"noseparator"}},
		"bad cursor":       {"cursor": {"!!"}},
		"from after to":    {"from": {"2026-10-10T11:00:00Z"}, "to": {"2026-10-10T10:00:00Z"}},
	} {
		code, _, body := getLogs(t, h, readToken, q)
		assert.Equal(t, http.StatusBadRequest, code, "%s: %s", name, body)
	}
}

func TestServicesEndpoint(t *testing.T) {
	h := newSearchHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/services?last=2h", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := decode[struct {
		Items []query.ServiceCount `json:"items"`
	}](t, rec)
	assert.Equal(t, []query.ServiceCount{{Service: "api", Logs: 2}, {Service: "worker", Logs: 1}}, got.Items)
}
