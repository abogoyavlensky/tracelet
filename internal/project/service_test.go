package project_test

import (
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T) (*project.Service, *clock) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	c := &clock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	return project.NewService(sqlite.NewProjectStore(db), c.now, rand.Reader), c
}

func TestCreateProject(t *testing.T) {
	svc, _ := newService(t)
	ctx := t.Context()

	p, err := svc.CreateProject(ctx, "shop", "Shop")
	require.NoError(t, err)
	assert.Len(t, string(p.ID), 16)
	assert.Equal(t, "Shop", p.Name)

	_, err = svc.CreateProject(ctx, "shop", "Other")
	require.ErrorIs(t, err, project.ErrSlugTaken)

	_, err = svc.CreateProject(ctx, "Bad Slug", "x")
	var verr *project.ValidationError
	require.ErrorAs(t, err, &verr)

	got, err := svc.FindBySlug(ctx, "shop")
	require.NoError(t, err)
	assert.Equal(t, p, got)
	_, err = svc.FindBySlug(ctx, "nope")
	require.ErrorIs(t, err, project.ErrNotFound)

	list, err := svc.ListProjects(ctx)
	require.NoError(t, err)
	assert.Equal(t, []project.Project{p}, list)
}

func TestCreateTokenRules(t *testing.T) {
	svc, _ := newService(t)
	ctx := t.Context()
	_, err := svc.CreateProject(ctx, "shop", "Shop")
	require.NoError(t, err)

	var verr *project.ValidationError
	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{Scope: project.ScopeIngest, Name: "x"})
	require.ErrorAs(t, err, &verr, "ingest needs a project")
	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{ProjectSlug: "shop", Scope: project.ScopeAdmin, Name: "x"})
	require.ErrorAs(t, err, &verr, "admin cannot have a project")
	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{ProjectSlug: "shop", Scope: "write", Name: "x"})
	require.ErrorAs(t, err, &verr, "unknown scope")
	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{ProjectSlug: "shop", Scope: project.ScopeIngest})
	require.ErrorAs(t, err, &verr, "name required")
	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{ProjectSlug: "nope", Scope: project.ScopeIngest, Name: "x"})
	require.ErrorIs(t, err, project.ErrNotFound)

	_, _, err = svc.CreateToken(ctx, project.NewTokenParams{Scope: project.ScopeRead, Name: "all projects"})
	require.NoError(t, err)
}

func TestAuthenticate(t *testing.T) {
	svc, c := newService(t)
	ctx := t.Context()
	p, err := svc.CreateProject(ctx, "shop", "Shop")
	require.NoError(t, err)

	tok, plaintext, err := svc.CreateToken(ctx, project.NewTokenParams{ProjectSlug: "shop", Scope: project.ScopeIngest, Name: "api"})
	require.NoError(t, err)
	assert.Equal(t, p.ID, tok.ProjectID)
	assert.Equal(t, plaintext[3:9], tok.Prefix)

	c.t = c.t.Add(time.Hour)
	got, err := svc.Authenticate(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, tok.ID, got.ID)
	assert.Equal(t, p.ID, got.ProjectID)
	assert.Equal(t, "shop", got.ProjectSlug)
	assert.Equal(t, c.t, got.LastUsedAt)

	list, err := svc.ListTokens(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, c.t, list[0].LastUsedAt, "last_used_at is stored")

	_, err = svc.Authenticate(ctx, "tl_0000000000000000000000000000000000000000")
	require.ErrorIs(t, err, project.ErrUnauthorized)
	_, err = svc.Authenticate(ctx, "")
	require.ErrorIs(t, err, project.ErrUnauthorized)

	require.NoError(t, svc.RevokeToken(ctx, tok.ID))
	_, err = svc.Authenticate(ctx, plaintext)
	require.ErrorIs(t, err, project.ErrUnauthorized)
	require.ErrorIs(t, svc.RevokeToken(ctx, "missing"), project.ErrNotFound)
}

func TestBootstrap(t *testing.T) {
	svc, _ := newService(t)
	ctx := t.Context()

	plaintext, err := svc.Bootstrap(ctx, "")
	require.NoError(t, err)
	require.NotEmpty(t, plaintext)
	tok, err := svc.Authenticate(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, project.ScopeAdmin, tok.Scope)

	again, err := svc.Bootstrap(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, again, "an admin token already exists")
}

func TestBootstrapWithSeed(t *testing.T) {
	svc, _ := newService(t)
	ctx := t.Context()
	seed := "tl_seededadmintoken0123456789"

	plaintext, err := svc.Bootstrap(ctx, seed)
	require.NoError(t, err)
	assert.Empty(t, plaintext)
	tok, err := svc.Authenticate(ctx, seed)
	require.NoError(t, err)
	assert.Equal(t, project.ScopeAdmin, tok.Scope)
	assert.Equal(t, "seeded", tok.Prefix)

	other, _ := newService(t)
	_, err = other.Bootstrap(ctx, "short")
	var verr *project.ValidationError
	require.ErrorAs(t, err, &verr)
}
