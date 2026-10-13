package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/query"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// metricFixture is newSearchHandler's metric points: a counter of api rising
// by 60 a minute, and one gauge point of worker with an attribute.
func metricFixture(ago func(m int) time.Time) []telemetry.MetricPoint {
	counter := func(m int, v int64) telemetry.MetricPoint {
		return telemetry.MetricPoint{
			TS: ago(m), StartTS: ago(120), Project: "p1", Service: "api", Name: "orders.created",
			Type: telemetry.TypeSum, Temporality: telemetry.TemporalityCumulative, Monotonic: true,
			SeriesHash: 1, ValueInt: &v,
		}
	}
	depth := 4.0
	return []telemetry.MetricPoint{
		counter(32, 60), counter(31, 120), counter(30, 180),
		{
			TS: ago(20), Project: "p1", Service: "worker", Name: "queue.depth", Type: telemetry.TypeGauge,
			SeriesHash: 2, ValueDouble: &depth, Attributes: `{"queue":"mail"}`,
		},
	}
}

func getMetrics(t *testing.T, h http.Handler, path string, q url.Values) (int, string) {
	t.Helper()
	rec := do(t, h, http.MethodGet, path+"?"+q.Encode(), readToken, "")
	return rec.Code, rec.Body.String()
}

func TestListMetrics(t *testing.T) {
	h := newSearchHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/metrics", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := decode[query.MetricsList](t, rec)
	require.Len(t, got.Items, 2)
	assert.Equal(t, "orders.created", got.Items[0].Name)
	assert.Equal(t, query.MetricKind{Type: query.KindSum, Monotonic: true}, got.Items[0].MetricKind)
	assert.Equal(t, query.Range{From: searchNow.Add(-time.Hour), To: searchNow}, got.Range, "default range is the last hour")

	rec = do(t, h, http.MethodGet, "/api/v1/metrics?service=worker&q=QUEUE", readToken, "")
	got = decode[query.MetricsList](t, rec)
	require.Len(t, got.Items, 1)
	assert.Equal(t, "queue.depth", got.Items[0].Name)
}

func TestMetricAttributes(t *testing.T) {
	h := newSearchHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/metrics/attributes?name=queue.depth", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := decode[query.MetricAttributes](t, rec)
	assert.Equal(t, []query.AttributeInfo{{Key: "queue", Distinct: 1, Values: []string{"mail"}}}, got.Items)

	code, body := getMetrics(t, h, "/api/v1/metrics/attributes", url.Values{})
	assert.Equal(t, http.StatusBadRequest, code, body)
	assert.Contains(t, body, "name is required")
}

func TestQueryMetric(t *testing.T) {
	h := newSearchHandler(t)
	rec := do(t, h, http.MethodGet, "/api/v1/metrics/query?name=orders.created&step=1m", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := decode[query.MetricResult](t, rec)
	assert.Equal(t, "rate", got.Aggregate, "a counter defaults to rate")
	assert.Equal(t, int64(60), got.StepSeconds)
	assert.Len(t, got.Timestamps, 60)
	require.Len(t, got.Series, 1)
	var rates []float64
	for _, v := range got.Series[0].Values {
		if v != nil {
			rates = append(rates, *v)
		}
	}
	assert.Equal(t, []float64{1, 1}, rates, "60 a minute is 1/s; the first point only seeds")

	q := url.Values{
		"name": {"queue.depth"}, "aggregate": {"max"}, "group_by": {"queue"},
		"attr": {"queue:mail"}, "service": {"worker"}, "step": {"5m"}, "last": {"2h"},
	}
	rec = do(t, h, http.MethodGet, "/api/v1/metrics/query?"+q.Encode(), readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got = decode[query.MetricResult](t, rec)
	assert.Equal(t, "max", got.Aggregate)
	assert.Equal(t, "queue", got.GroupBy)
	assert.Equal(t, int64(300), got.StepSeconds)
	require.Len(t, got.Series, 1)
	assert.Equal(t, map[string]string{"queue": "mail"}, got.Series[0].Group)
}

func TestQueryMetricErrors(t *testing.T) {
	h := newSearchHandler(t)
	for name, tc := range map[string]struct {
		q        url.Values
		want     int
		contains string
	}{
		"no name":        {url.Values{}, http.StatusBadRequest, "name is required"},
		"bad step":       {url.Values{"name": {"orders.created"}, "step": {"often"}}, http.StatusBadRequest, "step"},
		"short step":     {url.Values{"name": {"orders.created"}, "step": {"5s"}}, http.StatusBadRequest, "at least 10s"},
		"bad aggregate":  {url.Values{"name": {"orders.created"}, "aggregate": {"avg"}}, http.StatusBadRequest, "rate, increase"},
		"bad attr":       {url.Values{"name": {"orders.created"}, "attr": {"x"}}, http.StatusBadRequest, "key:value"},
		"unknown metric": {url.Values{"name": {"nope"}, "last": {"1h"}}, http.StatusNotFound, "between 2026-10-10T11:00:00Z and 2026-10-10T12:00:00Z"},
	} {
		code, body := getMetrics(t, h, "/api/v1/metrics/query", tc.q)
		assert.Equal(t, tc.want, code, "%s: %s", name, body)
		assert.Contains(t, body, tc.contains, name)
	}
}

func TestMetricRoutesNeedRead(t *testing.T) {
	h := newSearchHandler(t)
	for _, path := range []string{"/api/v1/metrics", "/api/v1/metrics/attributes?name=x", "/api/v1/metrics/query?name=x"} {
		rec := do(t, h, http.MethodGet, path, ingestToken, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, path)
	}
}
