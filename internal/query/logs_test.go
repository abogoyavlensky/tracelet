package query_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// searchEnv holds five logs of project p1 and one of p2: hour 0 flushed to
// cold, hour 1 hot. Two logs share a timestamp.
func searchEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	at := func(h time.Time, m int) time.Time { return h.Add(time.Duration(m) * time.Minute) }
	_, err := e.writer.Write(t.Context(), telemetry.Batch{Logs: []telemetry.Log{
		{
			TS: at(h0, 10), Project: "p1", Service: "api", Environment: "prod", SeverityNumber: 17, SeverityText: "ERROR",
			Body: "Payment FAILED", TraceID: "abc123", Attributes: `{"exception.type":"Timeout","http.route":"/pay"}`,
		},
		{TS: at(h0, 20), Project: "p1", Service: "worker", Environment: "prod", SeverityNumber: 9, Body: "job done"},
		{
			TS: at(h1, 5), Project: "p1", Service: "api", Environment: "staging", SeverityNumber: 13, Body: "slow request",
			Attributes: `{"exception.type":"Timeout","http.route":"/cart"}`, Resource: `{"service.name":"api"}`,
		},
		{TS: at(h1, 30), Project: "p1", Service: "api", Environment: "prod", SeverityNumber: 21, Body: "same ts a"},
		{TS: at(h1, 30), Project: "p1", Service: "api", Environment: "prod", SeverityNumber: 21, Body: "same ts b"},
		{TS: at(h1, 40), Project: "p2", Service: "api", Body: "other project"},
	}})
	require.NoError(t, err)
	_, err = e.flusher.FlushDue(t.Context(), h2)
	require.NoError(t, err)
	hours, err := e.store.HotHours(t.Context(), "logs")
	require.NoError(t, err)
	require.Equal(t, []time.Time{h1}, hours, "hour 0 is cold, hour 1 hot")
	return e
}

func search(t *testing.T, e *env, p query.LogsParams) query.LogsPage {
	t.Helper()
	if p.ProjectID == "" {
		p.ProjectID = "p1"
	}
	if p.Range == (query.Range{}) && p.Cursor == "" {
		p.Range = query.Range{From: h0, To: h2}
	}
	page, err := query.LogsSearch(t.Context(), e.open(t), p)
	require.NoError(t, err)
	return page
}

func bodies(page query.LogsPage) []string {
	out := []string{}
	for _, l := range page.Items {
		out = append(out, l.Body)
	}
	return out
}

func TestLogsSearchNewestFirst(t *testing.T) {
	e := searchEnv(t)
	page := search(t, e, query.LogsParams{})
	assert.Equal(t, []string{"same ts b", "same ts a", "slow request", "job done", "Payment FAILED"}, bodies(page))
	assert.Empty(t, page.NextCursor)
	assert.False(t, page.Truncated)
	assert.Equal(t, query.DefaultLimit, page.Limit)

	cold := page.Items[4]
	assert.Equal(t, "abc123", cold.TraceID)
	assert.JSONEq(t, `{"exception.type":"Timeout","http.route":"/pay"}`, string(cold.Attributes))
	assert.Nil(t, cold.Resource)
	assert.JSONEq(t, `{"service.name":"api"}`, string(page.Items[2].Resource))
}

func TestLogsSearchFilters(t *testing.T) {
	e := searchEnv(t)
	tests := []struct {
		name string
		p    query.LogsParams
		want []string
	}{
		{"service", query.LogsParams{Service: "worker"}, []string{"job done"}},
		{"environment", query.LogsParams{Environment: "staging"}, []string{"slow request"}},
		{"min level", query.LogsParams{MinSeverity: 17}, []string{"same ts b", "same ts a", "Payment FAILED"}},
		{"case-insensitive substring", query.LogsParams{Query: "payment fail"}, []string{"Payment FAILED"}},
		{"trace id", query.LogsParams{TraceID: "ABC123"}, []string{"Payment FAILED"}},
		{"dotted attr", query.LogsParams{Attrs: []query.Attr{{Key: "exception.type", Value: "Timeout"}}},
			[]string{"slow request", "Payment FAILED"}},
		{"two attrs", query.LogsParams{Attrs: []query.Attr{
			{Key: "exception.type", Value: "Timeout"}, {Key: "http.route", Value: "/pay"},
		}}, []string{"Payment FAILED"}},
		{"range", query.LogsParams{Range: query.Range{From: h1, To: h2}, Service: "api", MinSeverity: 13},
			[]string{"same ts b", "same ts a", "slow request"}},
		{"other project", query.LogsParams{ProjectID: "p2"}, []string{"other project"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bodies(search(t, e, tt.p)))
		})
	}
}

func TestLogsSearchAttrKeyIsLiteral(t *testing.T) {
	e := newEnv(t)
	_, err := e.writer.Write(t.Context(), telemetry.Batch{Logs: []telemetry.Log{
		{TS: h0, Project: "p1", Service: "api", Body: "nested", Attributes: `{"exception":{"type":"Timeout"}}`},
		{TS: h0, Project: "p1", Service: "api", Body: "quoted", Attributes: `{"a\"b":"x"}`},
	}})
	require.NoError(t, err)

	page := search(t, e, query.LogsParams{Attrs: []query.Attr{{Key: "exception.type", Value: "Timeout"}}})
	assert.Empty(t, page.Items, "a dotted key does not reach into nested objects")
	page = search(t, e, query.LogsParams{Attrs: []query.Attr{{Key: `a"b`, Value: "x"}}})
	assert.Equal(t, []string{"quoted"}, bodies(page))
}

func TestLogsSearchPagination(t *testing.T) {
	e := searchEnv(t)
	var got []string
	page := search(t, e, query.LogsParams{Limit: 2})
	got = append(got, bodies(page)...)
	pages := 1
	for page.NextCursor != "" {
		require.True(t, page.Truncated)
		page = search(t, e, query.LogsParams{Limit: 2, Cursor: page.NextCursor})
		got = append(got, bodies(page)...)
		pages++
	}
	assert.Equal(t, 3, pages)
	assert.Equal(t, []string{"same ts b", "same ts a", "slow request", "job done", "Payment FAILED"}, got,
		"every row exactly once, across the two sharing a timestamp")
}

func TestLogsSearchCursorKeepsRange(t *testing.T) {
	e := searchEnv(t)
	now := h1.Add(45 * time.Minute)
	rng, err := query.ResolveRange(time.Time{}, time.Time{}, time.Hour, now)
	require.NoError(t, err)
	first := search(t, e, query.LogsParams{Range: rng, Limit: 1})
	require.NotEmpty(t, first.NextCursor)
	assert.Equal(t, query.Range{From: now.Add(-time.Hour), To: now}, first.Range)

	// Time moves on and a newer row lands inside what "last hour" would mean now.
	_, err = e.writer.Write(t.Context(), telemetry.Batch{Logs: []telemetry.Log{
		{TS: now.Add(5 * time.Minute), Project: "p1", Service: "api", Body: "after the first page"},
	}})
	require.NoError(t, err)

	var got []string
	page := first
	for page.NextCursor != "" {
		page = search(t, e, query.LogsParams{Limit: 1, Cursor: page.NextCursor})
		assert.Equal(t, first.Range, page.Range)
		got = append(got, bodies(page)...)
	}
	assert.Equal(t, []string{"same ts b"}, bodies(first))
	assert.Equal(t, []string{"same ts a", "slow request"}, got, "later pages read the first page's window")
}

func TestLogsSearchValidation(t *testing.T) {
	e := searchEnv(t)
	snap := e.open(t)
	for name, p := range map[string]query.LogsParams{
		"limit too big": {ProjectID: "p1", Range: query.Range{From: h0, To: h1}, Limit: 1001},
		"bad cursor":    {ProjectID: "p1", Cursor: "!!"},
		"empty range":   {ProjectID: "p1", Range: query.Range{From: h1, To: h1}},
		"no project":    {Range: query.Range{From: h0, To: h1}},
	} {
		_, err := query.LogsSearch(t.Context(), snap, p)
		var verr *query.ValidationError
		assert.ErrorAs(t, err, &verr, name)
	}
}

func TestServices(t *testing.T) {
	e := searchEnv(t)
	got, err := query.Services(t.Context(), e.open(t), "p1", query.Range{From: h0, To: h2})
	require.NoError(t, err)
	assert.Equal(t, []query.ServiceCount{{Service: "api", Logs: 4}, {Service: "worker", Logs: 1}}, got)
}

func TestResolveRangeAndParsers(t *testing.T) {
	now := h2
	r, err := query.ResolveRange(time.Time{}, time.Time{}, 0, now)
	require.NoError(t, err)
	assert.Equal(t, query.Range{From: h1, To: h2}, r, "default is the last hour")
	r, err = query.ResolveRange(h0, time.Time{}, 0, now)
	require.NoError(t, err)
	assert.Equal(t, query.Range{From: h0, To: h2}, r)
	_, err = query.ResolveRange(h0, time.Time{}, time.Hour, now)
	require.Error(t, err)
	_, err = query.ResolveRange(h1, h0, 0, now)
	require.Error(t, err)

	n, err := query.ParseLevel("ERROR")
	require.NoError(t, err)
	assert.Equal(t, int16(17), n)
	_, err = query.ParseLevel("loud")
	require.Error(t, err)

	a, err := query.ParseAttr("http.url:https://x")
	require.NoError(t, err)
	assert.Equal(t, query.Attr{Key: "http.url", Value: "https://x"}, a)
	_, err = query.ParseAttr(":x")
	require.Error(t, err)
}
