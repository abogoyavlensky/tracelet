package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
)

type projectBody struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type tokenBody struct {
	ID         string `json:"id"`
	Project    string `json:"project"`
	Scope      string `json:"scope"`
	Prefix     string `json:"prefix"`
	Token      string `json:"token"`
	RevokedAt  string `json:"revoked_at"`
	LastUsedAt string `json:"last_used_at"`
}

func newAdminHandler(t *testing.T) http.Handler {
	t.Helper()
	return httpapi.NewHandler(httpapi.Deps{Auth: newFakeAuth(), Projects: newProjectService(t), Logger: discardLogger()})
}

func TestProjectsEndpoints(t *testing.T) {
	h := newAdminHandler(t)

	rec := do(t, h, http.MethodPost, "/api/v1/projects", adminToken, `{"slug":"shop","name":"Shop"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[projectBody](t, rec)
	assert.Equal(t, "shop", created.Slug)
	assert.Len(t, created.ID, 16)

	rec = do(t, h, http.MethodPost, "/api/v1/projects", adminToken, `{"slug":"shop","name":"Again"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)

	rec = do(t, h, http.MethodPost, "/api/v1/projects", adminToken, `{"slug":"Bad Slug"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decode[map[string]string](t, rec)["error"], "slug must be")

	rec = do(t, h, http.MethodPost, "/api/v1/projects", adminToken, `{"slug":"x","extra":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, h, http.MethodGet, "/api/v1/projects", adminToken, "")
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[struct{ Items []projectBody }](t, rec)
	assert.Equal(t, []projectBody{created}, list.Items)
}

func TestTokensEndpoints(t *testing.T) {
	h := newAdminHandler(t)
	require.Equal(t, http.StatusCreated, do(t, h, http.MethodPost, "/api/v1/projects", adminToken, `{"slug":"shop"}`).Code)

	rec := do(t, h, http.MethodPost, "/api/v1/tokens", adminToken, `{"project":"shop","scope":"ingest","name":"api server"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[tokenBody](t, rec)
	assert.Len(t, created.Token, 43)
	assert.Equal(t, created.Token[3:9], created.Prefix)
	assert.Equal(t, "shop", created.Project)
	assert.Empty(t, created.LastUsedAt)

	rec = do(t, h, http.MethodPost, "/api/v1/tokens", adminToken, `{"scope":"ingest","name":"x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = do(t, h, http.MethodPost, "/api/v1/tokens", adminToken, `{"project":"nope","scope":"read","name":"x"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = do(t, h, http.MethodGet, "/api/v1/tokens", adminToken, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), created.Token, "the plaintext is shown once")
	assert.NotContains(t, rec.Body.String(), "hash")
	list := decode[struct{ Items []tokenBody }](t, rec)
	require.Len(t, list.Items, 1)
	assert.Equal(t, created.Prefix, list.Items[0].Prefix)

	assert.Equal(t, http.StatusNoContent, do(t, h, http.MethodDelete, "/api/v1/tokens/"+created.ID, adminToken, "").Code)
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodDelete, "/api/v1/tokens/missing", adminToken, "").Code)

	list = decode[struct{ Items []tokenBody }](t, do(t, h, http.MethodGet, "/api/v1/tokens", adminToken, ""))
	assert.NotEmpty(t, list.Items[0].RevokedAt)
}
