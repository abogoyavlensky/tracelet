package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

// Authenticator resolves a bearer token's plaintext to the token.
type Authenticator interface {
	Authenticate(ctx context.Context, plaintext string) (project.Token, error)
}

type tokenKey struct{}

// requireScope authenticates the bearer token and lets the request through
// only when the token grants scope. The token rides on the request context
// (request-scoped metadata) for the handler's project checks.
func (h *Handler) requireScope(scope project.Scope, next http.HandlerFunc) http.HandlerFunc { //nolint:unparam // ingest and read routes arrive with OTLP and search
	return func(w http.ResponseWriter, r *http.Request) {
		plaintext, ok := bearer(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tracelet"`)
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tok, err := h.auth.Authenticate(r.Context(), plaintext)
		if errors.Is(err, project.ErrUnauthorized) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tracelet", error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, "invalid or revoked token")
			return
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		if !tok.Allows(scope) {
			writeError(w, http.StatusForbidden, "token lacks the "+string(scope)+" scope")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	}
}

// bearer reads the token from "Authorization: Bearer <token>".
func bearer(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
