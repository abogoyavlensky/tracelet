package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abogoyavlensky/tracelet/internal/httpapi"
)

func TestAuth(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{
		Auth: newFakeAuth(), Projects: newProjectService(t), Logger: discardLogger(),
	})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic tl_admin", http.StatusUnauthorized},
		{"unknown token", "Bearer tl_nope", http.StatusUnauthorized},
		{"wrong scope", "Bearer " + readToken, http.StatusForbidden},
		{"admin", "Bearer " + adminToken, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + adminToken, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := do(t, h, http.MethodGet, "/api/v1/projects", "", "")
			if tt.header != "" {
				r := newRequest(http.MethodGet, "/api/v1/projects")
				r.Header.Set("Authorization", tt.header)
				req = serve(h, r)
			}
			assert.Equal(t, tt.want, req.Code, req.Body.String())
			assert.Equal(t, "application/json", req.Header().Get("Content-Type"))
		})
	}
}

func TestHealthNeedsNoToken(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{Auth: newFakeAuth(), Logger: discardLogger()})
	assert.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/api/v1/health", "", "").Code)
}
