package dashboard_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/dashboard"
)

func metric(name string) *dashboard.MetricQuery { return &dashboard.MetricQuery{Name: name} }

func valid() dashboard.Definition {
	return dashboard.Definition{
		Title: "Shop",
		Panels: []dashboard.Panel{
			{Type: "timeseries", Metric: &dashboard.MetricQuery{Name: "orders.created", Aggregate: "rate", GroupBy: "service"}},
			{Type: "stat", Metric: &dashboard.MetricQuery{Name: "orders.created", Aggregate: "increase"}, Reduce: "sum"},
			{Type: "table", Metric: &dashboard.MetricQuery{Name: "http.server.request.duration", Aggregate: "p95", GroupBy: "http.route"}},
		},
	}
}

func TestNormalizeFillsDefaults(t *testing.T) {
	got, err := dashboard.Normalize(valid())
	require.NoError(t, err)
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "1h", got.Last)
	assert.Equal(t, "30s", got.Refresh)
	require.Len(t, got.Panels, 3)
	assert.Equal(t, []string{"p1", "p2", "p3"}, []string{got.Panels[0].ID, got.Panels[1].ID, got.Panels[2].ID})
	assert.Equal(t, []int{2, 1, 2}, []int{got.Panels[0].Width, got.Panels[1].Width, got.Panels[2].Width})
	assert.Equal(t, []string{"", "sum", "last"}, []string{got.Panels[0].Reduce, got.Panels[1].Reduce, got.Panels[2].Reduce})
}

func TestNormalizeAssignsFreeIDs(t *testing.T) {
	def := dashboard.Definition{Title: "x", Panels: []dashboard.Panel{
		{Type: "stat", Metric: metric("a")},
		{ID: "p1", Type: "stat", Metric: metric("b")},
		{Type: "stat", Metric: metric("c")},
		{ID: "p3", Type: "stat", Metric: metric("d")},
	}}
	got, err := dashboard.Normalize(def)
	require.NoError(t, err)
	ids := make([]string, 0, len(got.Panels))
	for _, p := range got.Panels {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"p2", "p1", "p4", "p3"}, ids)
}

func TestNormalizeIsIdempotent(t *testing.T) {
	once, err := dashboard.Normalize(valid())
	require.NoError(t, err)
	twice, err := dashboard.Normalize(once)
	require.NoError(t, err)
	a, _ := json.Marshal(once)
	b, _ := json.Marshal(twice)
	assert.Equal(t, string(a), string(b))
}

func TestNormalizeDoesNotModifyInput(t *testing.T) {
	def := valid()
	def.Panels[0].Metric.Attr = []string{"plan:pro"}
	before, _ := json.Marshal(def)
	_, err := dashboard.Normalize(def)
	require.NoError(t, err)
	after, _ := json.Marshal(def)
	assert.Equal(t, string(before), string(after))
}

func TestNormalizeDropsEmptyAttr(t *testing.T) {
	def := valid()
	def.Panels[0].Metric.Attr = []string{}
	got, err := dashboard.Normalize(def)
	require.NoError(t, err)
	assert.Nil(t, got.Panels[0].Metric.Attr)
}

func TestNormalizeRejects(t *testing.T) {
	cases := []struct {
		name   string
		change func(d *dashboard.Definition)
		msg    string
	}{
		{"no title", func(d *dashboard.Definition) { d.Title = "  " }, "title is required"},
		{"long title", func(d *dashboard.Definition) { d.Title = strings.Repeat("x", 201) }, "title must be at most 200"},
		{"schema", func(d *dashboard.Definition) { d.SchemaVersion = 2 }, "schema_version 2 is not supported"},
		{"last", func(d *dashboard.Definition) { d.Last = "yesterday" }, "last must be a positive duration"},
		{"negative last", func(d *dashboard.Definition) { d.Last = "-1h" }, "last must be a positive duration"},
		{"refresh", func(d *dashboard.Definition) { d.Refresh = "2s" }, "refresh must be one of off, 10s, 30s"},
		{"too many panels", func(d *dashboard.Definition) {
			d.Panels = make([]dashboard.Panel, 31)
		}, "at most 30 panels"},
		{"bad id", func(d *dashboard.Definition) { d.Panels[1].ID = "P 1" }, "panels[1].id must be"},
		{"duplicate id", func(d *dashboard.Definition) { d.Panels[0].ID = "a"; d.Panels[2].ID = "a" }, `panels[2].id "a" is used`},
		{"no type", func(d *dashboard.Definition) { d.Panels[0].Type = "" }, "panels[0].type is required"},
		{"bad type", func(d *dashboard.Definition) { d.Panels[0].Type = "pie" }, "panels[0].type must be"},
		{"width", func(d *dashboard.Definition) { d.Panels[0].Width = 5 }, "panels[0].width must be 1 to 4"},
		{"no metric", func(d *dashboard.Definition) { d.Panels[0].Metric = nil }, "panels[0].metric is required"},
		{"no name", func(d *dashboard.Definition) { d.Panels[0].Metric.Name = "" }, "panels[0].metric.name is required"},
		{"aggregate", func(d *dashboard.Definition) { d.Panels[0].Metric.Aggregate = "median" }, "panels[0].metric.aggregate must be"},
		{"attr", func(d *dashboard.Definition) { d.Panels[0].Metric.Attr = []string{"plan"} }, "panels[0].metric.attr[0] must be key:value"},
		{"short step", func(d *dashboard.Definition) { d.Panels[0].Metric.Step = "5s" }, "panels[0].metric.step must be whole seconds"},
		{"fractional step", func(d *dashboard.Definition) { d.Panels[0].Metric.Step = "10.5s" }, "panels[0].metric.step must be whole seconds"},
		{"long service", func(d *dashboard.Definition) { d.Panels[0].Metric.Service = strings.Repeat("s", 256) }, "panels[0].metric.service must be at most 255"},
		{"grouped stat", func(d *dashboard.Definition) { d.Panels[1].Metric.GroupBy = "service" }, "panels[1].metric.group_by: a stat panel"},
		{"reduce on timeseries", func(d *dashboard.Definition) { d.Panels[0].Reduce = "last" }, "panels[0].reduce applies only"},
		{"bad reduce", func(d *dashboard.Definition) { d.Panels[2].Reduce = "avg" }, "panels[2].reduce must be last or sum"},
		{"sum of rate", func(d *dashboard.Definition) { d.Panels[1].Metric.Aggregate = "rate" }, "panels[1].reduce sum needs metric.aggregate increase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := valid()
			tc.change(&def)
			_, err := dashboard.Normalize(def)
			verr, ok := errors.AsType[*dashboard.ValidationError](err)
			require.True(t, ok, "want a validation error, got %v", err)
			assert.Contains(t, verr.Message, tc.msg)
		})
	}
}

func TestValidateUID(t *testing.T) {
	require.NoError(t, dashboard.ValidateUID("shop-overview"))
	for _, uid := range []string{"", "-shop", "Shop", "shop overview", strings.Repeat("a", 65)} {
		assert.Error(t, dashboard.ValidateUID(uid), uid)
	}
}
