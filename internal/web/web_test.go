package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abogoyavlensky/tracelet/internal/web"
)

func builtFrontend() fstest.MapFS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<html>app</html>")},
		"favicon.svg":            {Data: []byte("<svg/>")},
		"assets/index-abc123.js": {Data: []byte("console.log(1)")},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	h := web.NewHandler(builtFrontend())

	rec := get(t, h, "/assets/index-abc123.js")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "console.log(1)", rec.Body.String())
}

func TestMissingAssetIsNotFoundAndNotCached(t *testing.T) {
	h := web.NewHandler(builtFrontend())

	for _, path := range []string{"/assets/index-old.js", "/assets/"} {
		rec := get(t, h, path)

		assert.Equal(t, http.StatusNotFound, rec.Code, path)
		assert.Empty(t, rec.Header().Get("Cache-Control"), path)
	}
}

func TestStaticFileIsServedWithoutLongCache(t *testing.T) {
	h := web.NewHandler(builtFrontend())

	rec := get(t, h, "/favicon.svg")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "<svg/>", rec.Body.String())
}

func TestUnknownPathServesIndex(t *testing.T) {
	h := web.NewHandler(builtFrontend())

	for _, path := range []string{"/", "/dashboards/42", "/index.html/nested"} {
		rec := get(t, h, path)

		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "<html>app</html>", rec.Body.String(), path)
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"), path)
	}
}

func TestUnbuiltFrontendIsNotFound(t *testing.T) {
	h := web.NewHandler(fstest.MapFS{".gitkeep": {}})

	rec := get(t, h, "/")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNonGetIsRejected(t *testing.T) {
	h := web.NewHandler(builtFrontend())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
