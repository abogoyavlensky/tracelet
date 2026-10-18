package cli_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dashboardsJSON = `{"items":[{"uid":"shop","title":"Shop overview","panels":3,"revision":2,"updated_at":"2026-10-18T09:00:00Z"}]}`
	dashboardJSON  = `{"uid":"shop","revision":2,"created_at":"2026-10-18T08:00:00Z","updated_at":"2026-10-18T09:00:00Z",` +
		`"title":"Shop overview","schema_version":1,"last":"6h","refresh":"30s",` +
		`"panels":[{"id":"p1","type":"timeseries","width":2,"metric":{"name":"orders.created","aggregate":"rate"}}]}`
)

func TestDashboardsList(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, out, errOut := run(t, "dashboards", "list", "--url", srv.URL, "--project", "shop")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, url.Values{"project": {"shop"}}, f.last.URL.Query())
	assert.Equal(t, "UID   TITLE          PANELS  REVISION  UPDATED\n"+
		"shop  Shop overview  3       2         2026-10-18T09:00:00.000Z\n", out)
}

func TestDashboardsExportDropsRevisionAndTimes(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)

	code, out, errOut := run(t, "dashboards", "export", "shop", "--url", srv.URL)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, `{
  "last": "6h",
  "panels": [
    {
      "id": "p1",
      "type": "timeseries",
      "width": 2,
      "metric": {
        "name": "orders.created",
        "aggregate": "rate"
      }
    }
  ],
  "refresh": "30s",
  "schema_version": 1,
  "title": "Shop overview",
  "uid": "shop"
}
`, out)
}

func TestDashboardsApply(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)
	file := filepath.Join(t.TempDir(), "shop.json")
	content := `{"uid": "shop", "title": "Shop overview", "panels": []}`
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))

	code, out, errOut := run(t, "dashboards", "apply", "--file", file, "--project", "shop", "--url", srv.URL)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "applied dashboard shop (revision 2)\n", out)
	assert.Equal(t, http.MethodPut, f.last.Method)
	assert.Equal(t, "/api/v1/dashboards/shop", f.last.URL.Path)
	assert.Equal(t, url.Values{"project": {"shop"}}, f.last.URL.Query())
	assert.JSONEq(t, content, f.body)
}

func TestDashboardsApplyNeedsAUID(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)
	file := filepath.Join(t.TempDir(), "shop.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"title": "x", "panels": []}`), 0o600))

	code, _, errOut := run(t, "dashboards", "apply", "--file", file, "--url", srv.URL)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "needs a uid")
}

func TestDashboardsApplyConflict(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	_, srv := newFakeAPI(t)
	file := filepath.Join(t.TempDir(), "stale.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"uid": "stale", "revision": 3, "title": "x", "panels": []}`), 0o600))

	code, _, errOut := run(t, "dashboards", "apply", "--file", file, "--url", srv.URL)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "dashboard stale is at revision 4, not 3")
}

func TestDashboardsDeleteNeedsYes(t *testing.T) {
	t.Setenv("TRACELET_TOKEN", "tl_good")
	f, srv := newFakeAPI(t)

	code, _, errOut := run(t, "dashboards", "delete", "shop", "--url", srv.URL)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "--yes")
	assert.Nil(t, f.last, "nothing was sent")

	code, out, errOut := run(t, "dashboards", "delete", "shop", "--yes", "--url", srv.URL)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "deleted dashboard shop\n", out)
	assert.Equal(t, http.MethodDelete, f.last.Method)

	code, out, errOut = run(t, "dashboards", "delete", "shop", "--yes", "--json", "--url", srv.URL)
	require.Equal(t, 0, code, errOut)
	assert.JSONEq(t, `{"uid":"shop","deleted":true}`, out)
}
