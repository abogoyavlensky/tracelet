package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/project"
	"github.com/abogoyavlensky/tracelet/internal/sqlite"
)

// fakeAuth maps plaintexts to tokens.
type fakeAuth map[string]project.Token

func (f fakeAuth) Authenticate(_ context.Context, plaintext string) (project.Token, error) {
	t, ok := f[plaintext]
	if !ok {
		return project.Token{}, project.ErrUnauthorized
	}
	return t, nil
}

// Tokens the fake knows.
const (
	adminToken  = "tl_admin"
	readToken   = "tl_read"
	ingestToken = "tl_ingest"
	readAll     = "tl_readall"
)

func newFakeAuth() fakeAuth {
	return fakeAuth{
		adminToken:  {ID: "a1", Scope: project.ScopeAdmin},
		readToken:   {ID: "r1", Scope: project.ScopeRead, ProjectID: "p1", ProjectSlug: "shop"},
		ingestToken: {ID: "i1", Scope: project.ScopeIngest, ProjectID: "p1", ProjectSlug: "shop"},
		readAll:     {ID: "r2", Scope: project.ScopeRead},
	}
}

func newProjectService(t *testing.T) *project.Service {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "tracelet.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = sqlite.Migrate(t.Context(), db)
	require.NoError(t, err)
	return project.NewService(sqlite.NewProjectStore(db), time.Now, rand.Reader)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// do sends a request with an optional bearer token and JSON body.
func do(t *testing.T, h http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func newRequest(method, target string) *http.Request { return httptest.NewRequest(method, target, nil) }

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}
