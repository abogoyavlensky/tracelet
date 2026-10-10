package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/abogoyavlensky/tracelet/api"
	"github.com/abogoyavlensky/tracelet/internal/httpapi"
)

func TestOpenAPIDescribesEveryRoute(t *testing.T) {
	var doc struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(api.OpenAPI, &doc))
	assert.Equal(t, "3.1.0", doc.OpenAPI)

	patterns := httpapi.NewHandler(httpapi.Deps{}).Patterns()
	require.NotEmpty(t, patterns)
	for _, p := range patterns {
		method, path, ok := strings.Cut(p, " ")
		require.True(t, ok, "pattern %q has a method", p)
		ops, ok := doc.Paths[path]
		if assert.True(t, ok, "%s is missing from api/openapi.yaml", path) {
			assert.Contains(t, ops, strings.ToLower(method), "%s is missing from api/openapi.yaml", p)
		}
	}
}

func TestOpenAPIServed(t *testing.T) {
	rec := do(t, httpapi.NewHandler(httpapi.Deps{}), http.MethodGet, "/api/v1/openapi.yaml", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/yaml", rec.Header().Get("Content-Type"))
	assert.Equal(t, api.OpenAPI, rec.Body.Bytes())
}
