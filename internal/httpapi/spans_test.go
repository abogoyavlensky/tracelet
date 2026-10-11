package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// Traces of newSearchHandler.
const (
	traceShop   = "4bf92f3577b34da6a3ce929d0e0e4736"
	traceHealth = "0af7651916cd43dd8448eb211c80319c"
	traceOld    = "5b8efff798038103d269b633813fc60c"
	rootShopID  = "00000000000000c1"
)

func testSpan(projectID, service, traceID, spanID, parent, name string, ts time.Time, d time.Duration, status int16) telemetry.Span {
	return telemetry.Span{
		TS: ts, EndTS: ts.Add(d), Project: projectID, Service: service, TraceID: traceID, SpanID: spanID,
		ParentSpanID: parent, Name: name, Kind: 2, StatusCode: status, DurationNS: int64(d),
	}
}

func getTrace(t *testing.T, h http.Handler, token, id string, q url.Values) (int, query.Trace, string) {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/traces/"+id+"?"+q.Encode(), token, "")
	var tr query.Trace
	if rec.Code == http.StatusOK {
		tr = decode[query.Trace](t, rec)
	}
	return rec.Code, tr, rec.Body.String()
}

func TestGetTrace(t *testing.T) {
	h := newSearchHandler(t)

	code, tr, body := getTrace(t, h, readToken, strings.ToUpper(traceShop), url.Values{})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, traceShop, tr.TraceID)
	require.Len(t, tr.Spans, 2, "the other project's span is not included")
	assert.Equal(t, "GET /pay", tr.Spans[0].Name)
	assert.Equal(t, rootShopID, tr.Spans[1].ParentSpanID)
	assert.Equal(t, []string{"api", "worker"}, tr.Services)
	assert.Equal(t, int64(600*time.Millisecond), tr.DurationNS)
	assert.Equal(t, query.Range{From: searchNow.Add(-7 * 24 * time.Hour), To: searchNow}, tr.Range, "default range is seven days")
	assert.False(t, tr.Truncated)

	code, _, _ = getTrace(t, h, readToken, traceOld, url.Values{})
	assert.Equal(t, http.StatusNotFound, code, "older than the default range")
	code, tr, body = getTrace(t, h, readToken, traceOld, url.Values{"last": {"720h"}})
	require.Equal(t, http.StatusOK, code, body)
	assert.Len(t, tr.Spans, 1, "a wider range finds it")

	code, _, _ = getTrace(t, h, readToken, traceShop, url.Values{"last": {"1h"}, "from": {"2026-10-10T11:00:00Z"}})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestGetTraceNotFound(t *testing.T) {
	h := newSearchHandler(t)
	code, _, body := getTrace(t, h, readToken, "ffffffffffffffffffffffffffffffff", url.Values{"last": {"1h"}})
	assert.Equal(t, http.StatusNotFound, code)
	assert.Contains(t, body, "between 2026-10-10T11:00:00Z and 2026-10-10T12:00:00Z")
}

func TestGetTraceBadID(t *testing.T) {
	h := newSearchHandler(t)
	for _, id := range []string{"abc", traceShop + "0", "zz" + traceShop[2:]} {
		code, _, body := getTrace(t, h, readToken, id, url.Values{})
		assert.Equal(t, http.StatusBadRequest, code, "%s: %s", id, body)
	}
}

func TestGetTraceProjectScope(t *testing.T) {
	h := newSearchHandler(t)
	code, _, _ := getTrace(t, h, readToken, traceShop, url.Values{"project": {"other"}})
	assert.Equal(t, http.StatusForbidden, code)

	code, tr, body := getTrace(t, h, readAll, traceShop, url.Values{"project": {"other"}})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, []string{"web"}, tr.Services)

	code, _, _ = getTrace(t, h, ingestToken, traceShop, url.Values{})
	assert.Equal(t, http.StatusForbidden, code)
}

func searchSpans(t *testing.T, h http.Handler, q url.Values) (int, query.SpansPage, string) {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/v1/spans?"+q.Encode(), readToken, "")
	var page query.SpansPage
	if rec.Code == http.StatusOK {
		page = decode[query.SpansPage](t, rec)
	}
	return rec.Code, page, rec.Body.String()
}

func spanNames(page query.SpansPage) []string {
	out := []string{}
	for _, s := range page.Items {
		out = append(out, s.Name)
	}
	return out
}

func TestSearchSpans(t *testing.T) {
	h := newSearchHandler(t)

	code, page, body := searchSpans(t, h, url.Values{})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, []string{"GET /health", "charge", "GET /pay"}, spanNames(page), "default range is the last hour")
	assert.Equal(t, query.Range{From: searchNow.Add(-time.Hour), To: searchNow}, page.Range)

	for name, tc := range map[string]struct {
		q    url.Values
		want []string
	}{
		"status":       {url.Values{"status": {"error"}}, []string{"GET /pay"}},
		"min duration": {url.Values{"min_duration": {"50ms"}}, []string{"GET /pay"}},
		"root":         {url.Values{"root": {"true"}}, []string{"GET /health", "GET /pay"}},
		"service":      {url.Values{"service": {"worker"}}, []string{"charge"}},
		"name":         {url.Values{"name": {"GET /health"}}, []string{"GET /health"}},
		"trace id":     {url.Values{"trace_id": {traceShop}}, []string{"charge", "GET /pay"}},
		"last":         {url.Values{"last": {"30m"}}, []string{"GET /health"}},
	} {
		code, page, body := searchSpans(t, h, tc.q)
		require.Equal(t, http.StatusOK, code, "%s: %s", name, body)
		assert.Equal(t, tc.want, spanNames(page), name)
	}

	_, page, _ = searchSpans(t, h, url.Values{"limit": {"2"}})
	require.NotEmpty(t, page.NextCursor)
	code, page, body = searchSpans(t, h, url.Values{"limit": {"2"}, "cursor": {page.NextCursor}})
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, []string{"GET /pay"}, spanNames(page))
}

func TestSearchSpansBadParameters(t *testing.T) {
	h := newSearchHandler(t)
	for name, q := range map[string]url.Values{
		"root maybe":        {"root": {"maybe"}},
		"bad status":        {"status": {"failed"}},
		"bad min duration":  {"min_duration": {"slow"}},
		"zero min duration": {"min_duration": {"0s"}},
		"cursor with last":  {"cursor": {"abc"}, "last": {"1h"}},
		"bad limit":         {"limit": {"many"}},
		"bad attr":          {"attr": {"noseparator"}},
		"bad cursor":        {"cursor": {"!!"}},
	} {
		code, _, body := searchSpans(t, h, q)
		assert.Equal(t, http.StatusBadRequest, code, "%s: %s", name, body)
	}
}
