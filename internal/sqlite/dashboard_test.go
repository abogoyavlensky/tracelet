package sqlite_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/dashboard"
	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

// newDashboardStore returns a store over a fresh database holding projects
// p1 (shop) and p2 (blog).
func newDashboardStore(t *testing.T) *sqlite.DashboardStore {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	projects := sqlite.NewProjectStore(db)
	require.NoError(t, projects.InsertProject(t.Context(), project.Project{ID: "p1", Slug: "shop", Name: "Shop", CreatedAt: created}))
	require.NoError(t, projects.InsertProject(t.Context(), project.Project{ID: "p2", Slug: "blog", Name: "Blog", CreatedAt: created}))
	return sqlite.NewDashboardStore(db)
}

func sampleDashboard(projectID, uid, title string) dashboard.Dashboard {
	return dashboard.Dashboard{
		ProjectID: projectID, UID: uid, Revision: 1, CreatedAt: created, UpdatedAt: created,
		Definition: dashboard.Definition{
			Title: title, SchemaVersion: 1, Last: "1h", Refresh: "30s",
			Panels: []dashboard.Panel{{
				ID: "p1", Type: "timeseries", Width: 2,
				Metric: &dashboard.MetricQuery{Name: "orders.created", Aggregate: "rate", Attr: []string{"plan:pro"}},
			}},
		},
	}
}

func TestDashboardStoreRoundTrip(t *testing.T) {
	s := newDashboardStore(t)
	ctx := t.Context()
	d := sampleDashboard("p1", "overview", "Overview")
	require.NoError(t, s.Insert(ctx, d))

	got, err := s.Get(ctx, "p1", "overview")
	require.NoError(t, err)
	assert.Equal(t, d, got)

	_, err = s.Get(ctx, "p1", "missing")
	require.ErrorIs(t, err, dashboard.ErrNotFound)
}

func TestDashboardStoreInsertConflict(t *testing.T) {
	s := newDashboardStore(t)
	ctx := t.Context()
	require.NoError(t, s.Insert(ctx, sampleDashboard("p1", "overview", "Overview")))
	err := s.Insert(ctx, sampleDashboard("p1", "overview", "Other"))
	require.ErrorIs(t, err, dashboard.ErrConflict)
	assert.EqualError(t, err, "dashboard overview already exists")
}

func TestDashboardStoreUpdateIsCompareAndSwap(t *testing.T) {
	s := newDashboardStore(t)
	ctx := t.Context()
	d := sampleDashboard("p1", "overview", "Overview")
	require.NoError(t, s.Insert(ctx, d))

	d.Title, d.Revision, d.UpdatedAt = "Renamed", 2, created.Add(time.Minute)
	require.NoError(t, s.Update(ctx, d, 1))
	got, err := s.Get(ctx, "p1", "overview")
	require.NoError(t, err)
	assert.Equal(t, d, got)

	stale := d
	stale.Title, stale.Revision = "Stale", 2
	require.ErrorIs(t, s.Update(ctx, stale, 1), dashboard.ErrConflict)

	require.NoError(t, s.Delete(ctx, "p1", "overview"))
	require.ErrorIs(t, s.Update(ctx, d, 2), dashboard.ErrConflict)
}

func TestDashboardStoreDelete(t *testing.T) {
	s := newDashboardStore(t)
	ctx := t.Context()
	require.NoError(t, s.Insert(ctx, sampleDashboard("p1", "overview", "Overview")))
	require.NoError(t, s.Delete(ctx, "p1", "overview"))
	require.ErrorIs(t, s.Delete(ctx, "p1", "overview"), dashboard.ErrNotFound)
}

func TestDashboardStoreSeparatesProjects(t *testing.T) {
	s := newDashboardStore(t)
	ctx := t.Context()
	require.NoError(t, s.Insert(ctx, sampleDashboard("p1", "overview", "Shop overview")))
	require.NoError(t, s.Insert(ctx, sampleDashboard("p2", "overview", "Blog overview")))
	require.NoError(t, s.Insert(ctx, sampleDashboard("p1", "a-latency", "Latency")))

	shop, err := s.List(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, shop, 2)
	assert.Equal(t, []string{"Latency", "Shop overview"}, []string{shop[0].Title, shop[1].Title})

	blog, err := s.Get(ctx, "p2", "overview")
	require.NoError(t, err)
	assert.Equal(t, "Blog overview", blog.Title)
}
