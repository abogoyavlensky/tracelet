package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
)

type dashboardBody struct {
	UID           string `json:"uid"`
	Revision      int    `json:"revision"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	Title         string `json:"title"`
	SchemaVersion int    `json:"schema_version"`
	Last          string `json:"last"`
	Refresh       string `json:"refresh"`
	Panels        []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Width  int    `json:"width"`
		Reduce string `json:"reduce"`
	} `json:"panels"`
}

const shopDashboard = `{"title":"Shop","panels":[
  {"type":"timeseries","metric":{"name":"orders.created","aggregate":"rate","group_by":"service"}},
  {"type":"stat","reduce":"sum","metric":{"name":"orders.created","aggregate":"increase"}}]}`

func newDashboardHandler(t *testing.T) http.Handler {
	t.Helper()
	projects, dashboards := newDashboardServices(t)
	return httpapi.NewHandler(httpapi.Deps{Auth: newFakeAuth(), Projects: projects, Dashboards: dashboards, Logger: discardLogger()})
}

func TestDashboardWritesAndRevisions(t *testing.T) {
	h := newDashboardHandler(t)

	rec := do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, shopDashboard)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[dashboardBody](t, rec)
	assert.Equal(t, "shop", created.UID)
	assert.Equal(t, 1, created.Revision)
	assert.Equal(t, 1, created.SchemaVersion)
	assert.Equal(t, "1h", created.Last)
	assert.Equal(t, "30s", created.Refresh)
	require.Len(t, created.Panels, 2)
	assert.Equal(t, "p1", created.Panels[0].ID)
	assert.Equal(t, 2, created.Panels[0].Width)
	assert.Equal(t, "sum", created.Panels[1].Reduce)

	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, shopDashboard)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, created, decode[dashboardBody](t, rec), "an unchanged write is a no-op")

	// What GET returns can be sent back as is.
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards/shop?project=shop", adminToken, "")
	require.Equal(t, http.StatusOK, rec.Code)
	got := rec.Body.String()
	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, got)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, decode[dashboardBody](t, rec).Revision)

	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, `{"revision":1,"title":"Renamed","panels":[]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, decode[dashboardBody](t, rec).Revision)

	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, `{"revision":1,"title":"Stale","panels":[]}`)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "dashboard shop is at revision 2, not 1; reload it and try again", decode[map[string]string](t, rec)["error"])

	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, `{"revision":0,"title":"New","panels":[]}`)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "dashboard shop already exists", decode[map[string]string](t, rec)["error"])
}

func TestDashboardBadRequests(t *testing.T) {
	h := newDashboardHandler(t)
	cases := []struct{ name, target, body, msg string }{
		{"uid mismatch", "/api/v1/dashboards/shop?project=shop", `{"uid":"other","title":"x","panels":[]}`, "does not match"},
		{"unknown field", "/api/v1/dashboards/shop?project=shop", `{"title":"x","panels":[],"colour":"red"}`, "unknown field"},
		{"validation", "/api/v1/dashboards/shop?project=shop", `{"title":"x","panels":[{"type":"pie","metric":{"name":"m"}}]}`, "panels[0].type must be"},
		{"bad uid", "/api/v1/dashboards/Shop?project=shop", `{"title":"x","panels":[]}`, "uid must be"},
		{"no project", "/api/v1/dashboards/shop", `{"title":"x","panels":[]}`, "project is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, http.MethodPut, tc.target, adminToken, tc.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, decode[map[string]string](t, rec)["error"], tc.msg)
		})
	}
}

func TestDashboardScopes(t *testing.T) {
	h := newDashboardHandler(t)
	rec := do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, shopDashboard)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// A read token limited to shop reads it without naming the project.
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards/shop", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards", readToken, "")
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[struct {
		Items []struct {
			UID    string `json:"uid"`
			Title  string `json:"title"`
			Panels int    `json:"panels"`
		} `json:"items"`
	}](t, rec)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "Shop", list.Items[0].Title)
	assert.Equal(t, 2, list.Items[0].Panels)

	rec = do(t, h, http.MethodGet, "/api/v1/dashboards?project=blog", readToken, "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards/shop?project=blog", readAll, "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "dashboards belong to one project")

	for _, tok := range []string{readToken, readAll, ingestToken} {
		rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", tok, shopDashboard)
		assert.Equal(t, http.StatusForbidden, rec.Code, tok)
		rec = do(t, h, http.MethodDelete, "/api/v1/dashboards/shop?project=shop", tok, "")
		assert.Equal(t, http.StatusForbidden, rec.Code, tok)
	}
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards?project=shop", ingestToken, "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDashboardDelete(t *testing.T) {
	h := newDashboardHandler(t)
	rec := do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, shopDashboard)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = do(t, h, http.MethodDelete, "/api/v1/dashboards/shop?project=shop", adminToken, "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = do(t, h, http.MethodGet, "/api/v1/dashboards/shop?project=shop", adminToken, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "dashboard not found", decode[map[string]string](t, rec)["error"])
	rec = do(t, h, http.MethodDelete, "/api/v1/dashboards/shop?project=shop", adminToken, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// A revision for a dashboard that is gone is a conflict, not a fresh create.
	rec = do(t, h, http.MethodPut, "/api/v1/dashboards/shop?project=shop", adminToken, `{"revision":1,"title":"x","panels":[]}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}
