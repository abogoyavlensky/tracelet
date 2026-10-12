package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
)

type sessionBody struct {
	Scope     string `json:"scope"`
	TokenName string `json:"token_name"`
	Project   string `json:"project"`
	Projects  []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"projects"`
}

// newSessionHandler returns a handler whose project store holds shop and blog.
func newSessionHandler(t *testing.T) http.Handler {
	t.Helper()
	projects := newProjectService(t)
	for _, slug := range []string{"shop", "blog"} {
		_, err := projects.CreateProject(t.Context(), slug, strings.ToUpper(slug[:1])+slug[1:])
		require.NoError(t, err)
	}
	return httpapi.NewHandler(httpapi.Deps{Auth: newFakeAuth(), Projects: projects, Logger: discardLogger()})
}

func signIn(t *testing.T, h http.Handler, token string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"token":"`+token+`"}`))
	for k, v := range header {
		r.Header[k] = v
	}
	return serve(h, r)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	raw := rec.Header().Values("Set-Cookie")
	require.Len(t, raw, 1, "one Set-Cookie header")
	c, err := http.ParseSetCookie(raw[0])
	require.NoError(t, err)
	return c
}

func withCookie(r *http.Request, value string) *http.Request {
	r.AddCookie(&http.Cookie{Name: "tracelet_session", Value: value})
	return r
}

func slugs(b sessionBody) []string {
	var out []string
	for _, p := range b.Projects {
		out = append(out, p.Slug)
	}
	return out
}

func TestSignInAdmin(t *testing.T) {
	h := newSessionHandler(t)

	rec := signIn(t, h, adminToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := decode[sessionBody](t, rec)
	assert.Equal(t, "admin", body.Scope)
	assert.Empty(t, body.Project)
	assert.ElementsMatch(t, []string{"shop", "blog"}, slugs(body))

	c := sessionCookie(t, rec)
	assert.Equal(t, "tracelet_session", c.Name)
	assert.Equal(t, adminToken, c.Value)
	assert.Equal(t, "/api/", c.Path)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Equal(t, 30*24*60*60, c.MaxAge)
	assert.False(t, c.Secure, "plain HTTP without a forwarded proto")
}

func TestSignInSecureBehindTLSProxy(t *testing.T) {
	h := newSessionHandler(t)
	rec := signIn(t, h, adminToken, http.Header{"X-Forwarded-Proto": {"https"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, sessionCookie(t, rec).Secure)
}

func TestSignInScopedRead(t *testing.T) {
	h := newSessionHandler(t)
	rec := signIn(t, h, readToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := decode[sessionBody](t, rec)
	assert.Equal(t, "read", body.Scope)
	assert.Equal(t, "shop", body.Project)
	assert.Equal(t, []string{"shop"}, slugs(body))
	assert.Equal(t, "Shop", body.Projects[0].Name)
}

func TestSignInAllProjectsRead(t *testing.T) {
	h := newSessionHandler(t)
	rec := signIn(t, h, readAll, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := decode[sessionBody](t, rec)
	assert.Empty(t, body.Project)
	assert.ElementsMatch(t, []string{"shop", "blog"}, slugs(body))
}

func TestSignInRefused(t *testing.T) {
	h := newSessionHandler(t)

	tests := []struct {
		name string
		body string
		want int
		msg  string
	}{
		{"ingest token", `{"token":"` + ingestToken + `"}`, http.StatusForbidden, "ingest tokens cannot sign in"},
		{"unknown token", `{"token":"tl_nope"}`, http.StatusUnauthorized, "invalid or revoked token"},
		{"empty token", `{"token":""}`, http.StatusUnauthorized, "invalid or revoked token"},
		{"malformed JSON", `{"token":`, http.StatusBadRequest, "invalid JSON body"},
		{"unknown field", `{"token":"tl_admin","x":1}`, http.StatusBadRequest, "invalid JSON body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, http.MethodPost, "/api/v1/session", "", tt.body)
			assert.Equal(t, tt.want, rec.Code, rec.Body.String())
			assert.Contains(t, decode[map[string]string](t, rec)["error"], tt.msg)
			assert.Empty(t, rec.Header().Values("Set-Cookie"))
		})
	}
}

func TestGetSession(t *testing.T) {
	h := newSessionHandler(t)

	rec := serve(h, withCookie(newRequest(http.MethodGet, "/api/v1/session"), readToken))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "shop", decode[sessionBody](t, rec).Project)

	rec = do(t, h, http.MethodGet, "/api/v1/session", adminToken, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "admin", decode[sessionBody](t, rec).Scope)

	rec = do(t, h, http.MethodGet, "/api/v1/session", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = serve(h, withCookie(newRequest(http.MethodGet, "/api/v1/session"), "tl_nope"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = do(t, h, http.MethodGet, "/api/v1/session", ingestToken, "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCookieAuthenticatesQueries(t *testing.T) {
	h := newSessionHandler(t)
	// The project list needs admin; a read cookie is refused, an admin one is not.
	rec := serve(h, withCookie(newRequest(http.MethodGet, "/api/v1/projects"), adminToken))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = serve(h, withCookie(newRequest(http.MethodGet, "/api/v1/projects"), readToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestBearerWinsOverCookie(t *testing.T) {
	h := newSessionHandler(t)
	r := withCookie(newRequest(http.MethodGet, "/api/v1/session"), adminToken)
	r.Header.Set("Authorization", "Bearer "+readToken)
	rec := serve(h, r)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "read", decode[sessionBody](t, rec).Scope)
}

func TestCookieCrossOriginWrites(t *testing.T) {
	h := newSessionHandler(t)

	post := func(site string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"slug":"new-`+site+`","name":"New"}`))
		r.Header.Set("Sec-Fetch-Site", site)
		return serve(h, withCookie(r, adminToken))
	}
	assert.Equal(t, http.StatusForbidden, post("cross-site").Code)
	assert.Equal(t, http.StatusCreated, post("same-origin").Code)

	// Bearer requests are not browser sessions and skip the check.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(`{"slug":"bearer","name":"New"}`))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Authorization", "Bearer "+adminToken)
	assert.Equal(t, http.StatusCreated, serve(h, r).Code)
}

func TestOTLPIgnoresCookie(t *testing.T) {
	h := newSessionHandler(t)
	r := withCookie(newRequest(http.MethodPost, "/v1/logs"), ingestToken)
	r.Header.Set("Content-Type", "application/x-protobuf")
	assert.Equal(t, http.StatusUnauthorized, serve(h, r).Code)
}

func TestSignOut(t *testing.T) {
	h := newSessionHandler(t)
	rec := do(t, h, http.MethodDelete, "/api/v1/session", "", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	c := sessionCookie(t, rec)
	assert.Equal(t, "tracelet_session", c.Name)
	assert.Empty(t, c.Value)
	assert.Equal(t, "/api/", c.Path)
	assert.Equal(t, -1, c.MaxAge, "Max-Age=0 parses as -1")
}
