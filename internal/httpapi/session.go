package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

// The web UI signs in by posting a token; the session cookie then carries
// that token. Revoking the token ends the session, so there is no session
// state on the server.
const (
	sessionCookieName = "tracelet_session"
	sessionCookiePath = "/api/"
	sessionMaxAge     = 30 * 24 * 60 * 60 // seconds
)

type signInRequest struct {
	Token string `json:"token"`
}

type projectRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// sessionResponse tells the UI who it is signed in as and which projects it
// may read. Project is set only for a project-scoped token.
type sessionResponse struct {
	Scope     project.Scope `json:"scope"`
	TokenName string        `json:"token_name"`
	Project   string        `json:"project,omitempty"`
	Projects  []projectRef  `json:"projects"`
}

// signIn checks a token and, for an admin or read token, sets the session
// cookie. Ingest tokens only write telemetry, so they cannot sign in.
func (h *Handler) signIn(w http.ResponseWriter, r *http.Request) {
	if err := h.crossOrigin.Check(r); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	var req signInRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusUnauthorized, "invalid or revoked token")
		return
	}
	tok, err := h.auth.Authenticate(r.Context(), req.Token)
	if errors.Is(err, project.ErrUnauthorized) {
		writeError(w, http.StatusUnauthorized, "invalid or revoked token")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if !tok.Allows(project.ScopeRead) {
		writeError(w, http.StatusForbidden, "ingest tokens cannot sign in")
		return
	}

	body, err := h.sessionBody(r.Context(), tok)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    req.Token,
		Path:     sessionCookiePath,
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   overTLS(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, body)
}

// session describes the signed-in token, from the cookie or a bearer header.
func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	body, err := h.sessionBody(r.Context(), tokenFrom(r.Context()))
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// signOut clears the session cookie. The token itself stays valid; revoking
// it is a separate, explicit act.
func (h *Handler) signOut(w http.ResponseWriter, r *http.Request) {
	if err := h.crossOrigin.Check(r); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     sessionCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   overTLS(r),
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// sessionBody lists the projects tok may read: its own for a project-scoped
// token, every project otherwise.
func (h *Handler) sessionBody(ctx context.Context, tok project.Token) (sessionResponse, error) {
	body := sessionResponse{Scope: tok.Scope, TokenName: tok.Name, Projects: []projectRef{}}
	if tok.ProjectSlug != "" {
		p, err := h.projects.FindBySlug(ctx, tok.ProjectSlug)
		if err != nil {
			return sessionResponse{}, fmt.Errorf("session project %s: %w", tok.ProjectSlug, err)
		}
		body.Project = p.Slug
		body.Projects = append(body.Projects, projectRef{Slug: p.Slug, Name: p.Name})
		return body, nil
	}
	all, err := h.projects.ListProjects(ctx)
	if err != nil {
		return sessionResponse{}, fmt.Errorf("session projects: %w", err)
	}
	for _, p := range all {
		body.Projects = append(body.Projects, projectRef{Slug: p.Slug, Name: p.Name})
	}
	return body, nil
}

// overTLS reports whether the browser reached us over HTTPS, directly or
// through a TLS-terminating proxy such as Caddy, so the cookie can be Secure
// there while plain-HTTP local runs still sign in.
func overTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
