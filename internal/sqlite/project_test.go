package sqlite_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

func newProjectStore(t *testing.T) *sqlite.ProjectStore {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	return sqlite.NewProjectStore(db)
}

var created = time.Date(2026, 10, 10, 12, 0, 0, 123000, time.UTC)

func TestProjectStoreRoundTrip(t *testing.T) {
	s := newProjectStore(t)
	ctx := t.Context()
	p := project.Project{ID: "0123456789abcdef", Slug: "shop", Name: "Shop", CreatedAt: created}
	require.NoError(t, s.InsertProject(ctx, p))
	require.ErrorIs(t, s.InsertProject(ctx, project.Project{ID: "other", Slug: "shop", CreatedAt: created}), project.ErrSlugTaken)

	got, err := s.ProjectBySlug(ctx, "shop")
	require.NoError(t, err)
	assert.Equal(t, p, got)
	_, err = s.ProjectBySlug(ctx, "missing")
	require.ErrorIs(t, err, project.ErrNotFound)

	tok := project.Token{ID: "t1", ProjectID: p.ID, Scope: project.ScopeIngest, Name: "api", Prefix: "abcdef", CreatedAt: created}
	hash := project.HashToken("tl_x")
	require.NoError(t, s.InsertToken(ctx, tok, hash))

	got2, stored, err := s.TokenByHash(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, hash, stored)
	tok.ProjectSlug = "shop"
	assert.Equal(t, tok, got2)
	_, _, err = s.TokenByHash(ctx, project.HashToken("tl_y"))
	require.ErrorIs(t, err, project.ErrNotFound)
}

func TestProjectStoreRevokeKeepsFirstTime(t *testing.T) {
	s := newProjectStore(t)
	ctx := t.Context()
	tok := project.Token{ID: "t1", Scope: project.ScopeAdmin, Name: "admin", Prefix: "abcdef", CreatedAt: created}
	require.NoError(t, s.InsertToken(ctx, tok, project.HashToken("tl_admin")))

	n, err := s.CountActiveAdminTokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	first := created.Add(time.Hour)
	require.NoError(t, s.RevokeToken(ctx, "t1", first))
	require.NoError(t, s.RevokeToken(ctx, "t1", first.Add(time.Hour)))
	list, err := s.ListTokens(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, first, list[0].RevokedAt)
	assert.Empty(t, list[0].ProjectSlug)

	n, err = s.CountActiveAdminTokens(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	require.ErrorIs(t, s.RevokeToken(ctx, "missing", first), project.ErrNotFound)
}
