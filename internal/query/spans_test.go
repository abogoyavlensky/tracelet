package query_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// Trace and span IDs of the spans fixture.
const (
	trace1 = "4bf92f3577b34da6a3ce929d0e0e4736"
	trace2 = "0af7651916cd43dd8448eb211c80319c"

	rootID   = "00f067aa0ba902b7"
	clientID = "b7ad6b7169203331"
	workerID = "53995c3f42cd8ad8"
	tickID   = "c3f4c1e0b1a2d3e4"
	otherID  = "e457b5a2e4d86bd1"
)

// The fixture's p1 spans. trace1's root and client span are the last seconds
// of hour 0, which is flushed; its worker span and all of trace2 are in hour
// 1, which stays hot, so trace1 straddles cold and hot.
var (
	rootSpan = telemetry.Span{
		TS: h0.Add(59*time.Minute + 59*time.Second), Project: "p1", Service: "api", Environment: "prod", Version: "1.4.0",
		TraceID: trace1, SpanID: rootID, Name: "GET /cart", Kind: 2, StatusCode: 2, StatusMessage: "deadline exceeded",
		DurationNS: int64(120 * time.Millisecond), Scope: "http", Resource: `{"service.name":"api"}`,
		Attributes: `{"http.route":"/cart"}`, Events: `[{"ts":"2026-10-01T00:59:59.1Z","name":"retry"}]`,
	}
	clientSpan = telemetry.Span{
		TS: rootSpan.TS.Add(10 * time.Millisecond), Project: "p1", Service: "api", Environment: "prod",
		TraceID: trace1, SpanID: clientID, ParentSpanID: rootID, Name: "db.query", Kind: 3, StatusCode: 1,
		DurationNS: int64(80 * time.Millisecond),
	}
	workerSpan = telemetry.Span{
		TS: h1.Add(time.Second), Project: "p1", Service: "worker", Environment: "staging",
		TraceID: trace1, SpanID: workerID, ParentSpanID: clientID, Name: "process", Kind: 5, StatusCode: 2,
		DurationNS: int64(40 * time.Millisecond),
		Links:      `[{"trace_id":"0af7651916cd43dd8448eb211c80319c","span_id":"c3f4c1e0b1a2d3e4"}]`,
	}
	tickSpan = telemetry.Span{
		TS: h1.Add(10 * time.Minute), Project: "p1", Service: "worker", Environment: "prod",
		TraceID: trace2, SpanID: tickID, Name: "tick", Kind: 1, DurationNS: int64(5 * time.Millisecond),
	}
	otherProjectSpan = telemetry.Span{
		TS: h1.Add(20 * time.Minute), Project: "p2", Service: "api",
		TraceID: trace1, SpanID: otherID, Name: "GET /cart", Kind: 2, DurationNS: int64(time.Millisecond),
	}
)

func withEnd(spans ...telemetry.Span) []telemetry.Span {
	for i := range spans {
		spans[i].EndTS = spans[i].TS.Add(time.Duration(spans[i].DurationNS))
	}
	return spans
}

// writeSpans writes the fixture and flushes hour 0.
func writeSpans(t *testing.T, e *env) {
	t.Helper()
	_, err := e.writer.Write(t.Context(), telemetry.Batch{
		Spans: withEnd(rootSpan, clientSpan, workerSpan, tickSpan, otherProjectSpan),
	})
	require.NoError(t, err)
	_, err = e.flusher.FlushDue(t.Context(), h2)
	require.NoError(t, err)
	hours, err := e.store.HotHours(t.Context(), "spans")
	require.NoError(t, err)
	require.Equal(t, []time.Time{h1}, hours, "hour 0 is cold, hour 1 hot")
}

func spansEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	writeSpans(t, e)
	return e
}

func spanIDs(spans []query.Span) []string {
	out := []string{}
	for _, s := range spans {
		out = append(out, s.SpanID)
	}
	return out
}

func TestTraceGet(t *testing.T) {
	e := spansEnv(t)
	rng := query.Range{From: h0, To: h2}
	tr, err := query.TraceGet(t.Context(), e.open(t), query.TraceParams{ProjectID: "p1", TraceID: strings.ToUpper(trace1), Range: rng})
	require.NoError(t, err)

	assert.Equal(t, trace1, tr.TraceID)
	assert.Equal(t, []string{rootID, clientID, workerID}, spanIDs(tr.Spans), "oldest first, across cold and hot")
	assert.Equal(t, []string{"api", "worker"}, tr.Services)
	end := workerSpan.TS.Add(40 * time.Millisecond)
	assert.Equal(t, rootSpan.TS, tr.Start)
	assert.Equal(t, end, tr.End)
	assert.Equal(t, end.Sub(rootSpan.TS).Nanoseconds(), tr.DurationNS)
	assert.False(t, tr.Truncated)
	assert.Equal(t, rng, tr.Range)

	root := tr.Spans[0]
	assert.Equal(t, rootSpan.TS.Add(120*time.Millisecond), root.EndTS)
	assert.Equal(t, int64(120*time.Millisecond), root.DurationNS)
	assert.Equal(t, "api", root.Service)
	assert.Equal(t, "prod", root.Environment)
	assert.Equal(t, "1.4.0", root.Version)
	assert.Empty(t, root.ParentSpanID)
	assert.Equal(t, "GET /cart", root.Name)
	assert.Equal(t, int16(2), root.Kind)
	assert.Equal(t, int16(2), root.StatusCode)
	assert.Equal(t, "deadline exceeded", root.StatusMessage)
	assert.Equal(t, "http", root.Scope)
	assert.JSONEq(t, `{"service.name":"api"}`, string(root.Resource))
	assert.JSONEq(t, `{"http.route":"/cart"}`, string(root.Attributes))
	assert.JSONEq(t, rootSpan.Events, string(root.Events))
	assert.Nil(t, root.Links)
	assert.Equal(t, clientID, tr.Spans[2].ParentSpanID)
	assert.JSONEq(t, workerSpan.Links, string(tr.Spans[2].Links))
}

func TestTraceGetNotFound(t *testing.T) {
	e := spansEnv(t)
	_, err := query.TraceGet(t.Context(), e.open(t), query.TraceParams{
		ProjectID: "p1", TraceID: trace1, Range: query.Range{From: h2, To: h2.Add(time.Hour)},
	})
	require.ErrorIs(t, err, query.ErrTraceNotFound)

	_, err = query.TraceGet(t.Context(), e.open(t), query.TraceParams{
		ProjectID: "p1", TraceID: trace1[:31], Range: query.Range{From: h0, To: h2},
	})
	var verr *query.ValidationError
	require.ErrorAs(t, err, &verr)
}

func TestTraceGetTruncates(t *testing.T) {
	e := newEnv(t)
	var spans []telemetry.Span
	for i := range 4 {
		spans = append(spans, telemetry.Span{
			TS: h0.Add(time.Duration(i) * time.Second), Project: "p1", Service: fmt.Sprintf("svc-%d", 3-i),
			TraceID: trace2, SpanID: fmt.Sprintf("%016x", i+1), Name: "op", DurationNS: int64(time.Millisecond),
		})
	}
	_, err := e.writer.Write(t.Context(), telemetry.Batch{Spans: withEnd(spans...)})
	require.NoError(t, err)

	tr, err := query.TraceGet(t.Context(), e.open(t), query.TraceParams{
		ProjectID: "p1", TraceID: trace2, Range: query.Range{From: h0, To: h1}, Limit: 3,
	})
	require.NoError(t, err)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"0000000000000001", "0000000000000002", "0000000000000003"}, spanIDs(tr.Spans))
	assert.Equal(t, h0.Add(2*time.Second+time.Millisecond), tr.End, "the summary describes the returned spans")
	assert.Equal(t, []string{"svc-1", "svc-2", "svc-3"}, tr.Services)
}

func searchSpans(t *testing.T, e *env, p query.SpansParams) query.SpansPage {
	t.Helper()
	if p.ProjectID == "" {
		p.ProjectID = "p1"
	}
	if p.Range == (query.Range{}) && p.Cursor == "" {
		p.Range = query.Range{From: h0, To: h2}
	}
	page, err := query.SpansSearch(t.Context(), e.open(t), p)
	require.NoError(t, err)
	return page
}

func status(code int16) *int16 { return &code }

func TestSpansSearchFilters(t *testing.T) {
	e := spansEnv(t)
	tests := []struct {
		name string
		p    query.SpansParams
		want []string
	}{
		{"all, newest first", query.SpansParams{}, []string{tickID, workerID, clientID, rootID}},
		{"service", query.SpansParams{Service: "api"}, []string{clientID, rootID}},
		{"environment", query.SpansParams{Environment: "staging"}, []string{workerID}},
		{"name", query.SpansParams{Name: "process"}, []string{workerID}},
		{"status error", query.SpansParams{Status: status(2)}, []string{workerID, rootID}},
		{"status ok", query.SpansParams{Status: status(1)}, []string{clientID}},
		{"status unset", query.SpansParams{Status: status(0)}, []string{tickID}},
		{"min duration", query.SpansParams{MinDurationNS: int64(50 * time.Millisecond)}, []string{clientID, rootID}},
		{"root", query.SpansParams{Root: true}, []string{tickID, rootID}},
		{"trace id", query.SpansParams{TraceID: strings.ToUpper(trace1)}, []string{workerID, clientID, rootID}},
		{"attr", query.SpansParams{Attrs: []query.Attr{{Key: "http.route", Value: "/cart"}}}, []string{rootID}},
		{"range", query.SpansParams{Range: query.Range{From: h1, To: h2}}, []string{tickID, workerID}},
		{"other project", query.SpansParams{ProjectID: "p2"}, []string{otherID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := searchSpans(t, e, tt.p)
			assert.Equal(t, tt.want, spanIDs(page.Items))
			assert.False(t, page.Truncated)
		})
	}
}

func TestSpansSearchPagination(t *testing.T) {
	e := spansEnv(t)
	page := searchSpans(t, e, query.SpansParams{Limit: 2})
	require.True(t, page.Truncated)
	require.NotEmpty(t, page.NextCursor)
	got := spanIDs(page.Items)

	page = searchSpans(t, e, query.SpansParams{Limit: 2, Cursor: page.NextCursor})
	got = append(got, spanIDs(page.Items)...)
	assert.Empty(t, page.NextCursor)
	assert.Equal(t, query.Range{From: h0, To: h2}, page.Range, "the cursor carries the range")
	assert.Equal(t, []string{tickID, workerID, clientID, rootID}, got)
}

func TestSpansSearchValidation(t *testing.T) {
	e := spansEnv(t)
	snap := e.open(t)
	rng := query.Range{From: h0, To: h1}
	for name, p := range map[string]query.SpansParams{
		"negative min duration": {ProjectID: "p1", Range: rng, MinDurationNS: -1},
		"bad cursor":            {ProjectID: "p1", Cursor: "!!"},
		"limit too big":         {ProjectID: "p1", Range: rng, Limit: 1001},
		"empty range":           {ProjectID: "p1", Range: query.Range{From: h1, To: h1}},
		"no project":            {Range: rng},
	} {
		_, err := query.SpansSearch(t.Context(), snap, p)
		var verr *query.ValidationError
		assert.ErrorAs(t, err, &verr, name)
	}
}

func TestParseStatusAndTraceID(t *testing.T) {
	for s, want := range map[string]int16{"unset": 0, "ok": 1, "ERROR": 2} {
		code, ok, err := query.ParseStatus(s)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, want, code, s)
	}
	_, ok, err := query.ParseStatus("")
	require.NoError(t, err)
	assert.False(t, ok, "empty is any status")
	_, _, err = query.ParseStatus("failed")
	var verr *query.ValidationError
	require.ErrorAs(t, err, &verr)

	id, err := query.ParseTraceID(strings.ToUpper(trace1))
	require.NoError(t, err)
	assert.Equal(t, trace1, id)
	for _, bad := range []string{"", trace1[:31], trace1 + "0", "zz" + trace1[2:]} {
		_, err := query.ParseTraceID(bad)
		require.ErrorAs(t, err, &verr, bad)
	}
}

func TestServicesCountsSpans(t *testing.T) {
	e := searchEnv(t)
	writeSpans(t, e)
	got, err := query.Services(t.Context(), e.open(t), "p1", query.Range{From: h0, To: h2})
	require.NoError(t, err)
	assert.Equal(t, []query.ServiceCount{
		{Service: "api", Logs: 4, Spans: 2},
		{Service: "worker", Logs: 1, Spans: 2},
	}, got)
}

func TestServicesSpansOnly(t *testing.T) {
	e := spansEnv(t)
	got, err := query.Services(t.Context(), e.open(t), "p1", query.Range{From: h0, To: h2})
	require.NoError(t, err)
	assert.Equal(t, []query.ServiceCount{
		{Service: "api", Spans: 2},
		{Service: "worker", Spans: 2},
	}, got)
}
