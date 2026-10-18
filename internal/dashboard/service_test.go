package dashboard_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/dashboard"
	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

var t0 = time.Date(2026, 10, 18, 9, 0, 0, 123456789, time.UTC)

// clock returns a clock that advances a minute per call.
func clock() func() time.Time {
	now := t0
	return func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
}

func newStore(t *testing.T) *sqlite.DashboardStore {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	require.NoError(t, sqlite.NewProjectStore(db).InsertProject(t.Context(),
		project.Project{ID: "p1", Slug: "shop", Name: "Shop", CreatedAt: t0}))
	return sqlite.NewDashboardStore(db)
}

func TestPutCreatesThenKeepsAnUnchangedRevision(t *testing.T) {
	s := dashboard.NewService(newStore(t), clock())
	ctx := t.Context()

	d, created, err := s.Put(ctx, "p1", "shop", valid(), nil)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, 1, d.Revision)
	got, err := s.Get(ctx, "p1", "shop")
	require.NoError(t, err)
	assert.Equal(t, d, got)

	again, created, err := s.Put(ctx, "p1", "shop", valid(), nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, d, again, "an unchanged write keeps the revision and updated_at")

	changed := valid()
	changed.Title = "Shop, renamed"
	d2, created, err := s.Put(ctx, "p1", "shop", changed, nil)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, 2, d2.Revision)
	assert.Equal(t, d.CreatedAt, d2.CreatedAt)
	assert.True(t, d2.UpdatedAt.After(d.UpdatedAt))
}

func TestPutRevisionChecks(t *testing.T) {
	s := dashboard.NewService(newStore(t), clock())
	ctx := t.Context()
	_, _, err := s.Put(ctx, "p1", "shop", valid(), new(0))
	require.NoError(t, err)

	_, _, err = s.Put(ctx, "p1", "shop", valid(), new(0))
	require.ErrorIs(t, err, dashboard.ErrConflict)
	assert.EqualError(t, err, "dashboard shop already exists")

	changed := valid()
	changed.Title = "Renamed"
	_, _, err = s.Put(ctx, "p1", "shop", changed, new(1))
	require.NoError(t, err)

	_, _, err = s.Put(ctx, "p1", "shop", valid(), new(1))
	require.ErrorIs(t, err, dashboard.ErrConflict)
	assert.EqualError(t, err, "dashboard shop is at revision 2, not 1; reload it and try again")

	require.NoError(t, s.Delete(ctx, "p1", "shop"))
	_, _, err = s.Put(ctx, "p1", "shop", valid(), new(2))
	require.ErrorIs(t, err, dashboard.ErrConflict)
	assert.EqualError(t, err, "dashboard shop no longer exists")
}

func TestPutValidates(t *testing.T) {
	s := dashboard.NewService(newStore(t), clock())
	_, _, err := s.Put(t.Context(), "p1", "Shop!", valid(), nil)
	assert.ErrorAs(t, err, new(*dashboard.ValidationError))

	bad := valid()
	bad.Title = ""
	_, _, err = s.Put(t.Context(), "p1", "shop", bad, nil)
	assert.ErrorAs(t, err, new(*dashboard.ValidationError))
}

func TestListOrdersByTitle(t *testing.T) {
	s := dashboard.NewService(newStore(t), clock())
	ctx := t.Context()
	for uid, title := range map[string]string{"b": "Latency", "a": "Orders", "c": "Errors"} {
		def := valid()
		def.Title = title
		_, _, err := s.Put(ctx, "p1", uid, def, nil)
		require.NoError(t, err)
	}
	list, err := s.List(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, []string{"Errors", "Latency", "Orders"}, []string{list[0].Title, list[1].Title, list[2].Title})
	assert.Equal(t, 3, list[0].Panels)
}

// racingStore lets another writer in between the service's read and its
// write.
type racingStore struct {
	*sqlite.DashboardStore
	race func(ctx context.Context)
}

func (r racingStore) Get(ctx context.Context, projectID, uid string) (dashboard.Dashboard, error) {
	d, err := r.DashboardStore.Get(ctx, projectID, uid)
	if r.race != nil {
		r.race(ctx)
	}
	return d, err
}

func TestPutLosesARaceWithoutOverwriting(t *testing.T) {
	store := newStore(t)
	ctx := t.Context()
	_, _, err := dashboard.NewService(store, clock()).Put(ctx, "p1", "shop", valid(), nil)
	require.NoError(t, err)

	other := dashboard.NewService(store, clock())
	racing := dashboard.NewService(racingStore{store, func(ctx context.Context) {
		def := valid()
		def.Title = "The other writer"
		_, _, err := other.Put(ctx, "p1", "shop", def, nil)
		require.NoError(t, err)
	}}, clock())

	mine := valid()
	mine.Title = "Mine"
	_, _, err = racing.Put(ctx, "p1", "shop", mine, nil)
	require.ErrorIs(t, err, dashboard.ErrConflict)

	got, err := store.Get(ctx, "p1", "shop")
	require.NoError(t, err)
	assert.Equal(t, "The other writer", got.Title)
	assert.Equal(t, 2, got.Revision)
}

func TestPutLosesACreateRace(t *testing.T) {
	store := newStore(t)
	ctx := t.Context()
	other := dashboard.NewService(store, clock())
	racing := dashboard.NewService(racingStore{store, func(ctx context.Context) {
		def := valid()
		def.Title = "First"
		_, _, err := other.Put(ctx, "p1", "shop", def, nil)
		require.NoError(t, err)
	}}, clock())

	_, _, err := racing.Put(ctx, "p1", "shop", valid(), nil)
	require.ErrorIs(t, err, dashboard.ErrConflict)
	got, err := store.Get(ctx, "p1", "shop")
	require.NoError(t, err)
	assert.Equal(t, "First", got.Title)
}
