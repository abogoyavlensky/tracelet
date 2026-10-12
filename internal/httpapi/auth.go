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

// tokenFrom returns the authenticated token requireScope stored on the
// request context.
func tokenFrom(ctx context.Context) project.Token {
	t, _ := ctx.Value(tokenKey{}).(project.Token)
	return t
}

// requireScope authenticates the request's credential and lets the request
// through only when the token grants scope. The token rides on the request
// context (request-scoped metadata) for the handler's project checks.
//
// A credential from the session cookie is a browser session, so a non-safe
// cross-origin request carrying it is refused before the token is looked up.
func (h *Handler) requireScope(scope project.Scope, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plaintext, fromCookie, ok := credential(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tracelet"`)
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		if fromCookie {
			if err := h.crossOrigin.Check(r); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
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

// credential reads the token from "Authorization: Bearer <token>", or, for
// the management API only, from the session cookie. The bearer header wins
// when both are present. OTLP routes never accept the cookie.
func credential(r *http.Request) (plaintext string, fromCookie, ok bool) {
	if token, ok := bearer(r); ok {
		return token, false, true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return "", false, false
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false, false
	}
	return c.Value, true, true
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
