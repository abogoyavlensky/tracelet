package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/project"
)

// Projects is what the admin endpoints call.
type Projects interface {
	CreateProject(ctx context.Context, slug, name string) (project.Project, error)
	ListProjects(ctx context.Context) ([]project.Project, error)
	FindBySlug(ctx context.Context, slug string) (project.Project, error)
	CreateToken(ctx context.Context, p project.NewTokenParams) (project.Token, string, error)
	ListTokens(ctx context.Context) ([]project.Token, error)
	RevokeToken(ctx context.Context, id project.TokenID) error
}

type projectJSON struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func toProjectJSON(p project.Project) projectJSON {
	return projectJSON{ID: string(p.ID), Slug: p.Slug, Name: p.Name, CreatedAt: p.CreatedAt}
}

type tokenJSON struct {
	ID         string    `json:"id"`
	Project    string    `json:"project,omitempty"`
	Scope      string    `json:"scope"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at,omitzero"`
	RevokedAt  time.Time `json:"revoked_at,omitzero"`
}

func toTokenJSON(t project.Token) tokenJSON {
	return tokenJSON{
		ID: string(t.ID), Project: t.ProjectSlug, Scope: string(t.Scope), Name: t.Name, Prefix: t.Prefix,
		CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
	}
}

type listResponse[T any] struct {
	Items []T `json:"items"`
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.ListProjects(r.Context())
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	items := make([]projectJSON, 0, len(projects))
	for _, p := range projects {
		items = append(items, toProjectJSON(p))
	}
	writeJSON(w, http.StatusOK, listResponse[projectJSON]{Items: items})
}

type createProjectRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := h.projects.CreateProject(r.Context(), req.Slug, req.Name)
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProjectJSON(p))
}

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.projects.ListTokens(r.Context())
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	items := make([]tokenJSON, 0, len(tokens))
	for _, t := range tokens {
		items = append(items, toTokenJSON(t))
	}
	writeJSON(w, http.StatusOK, listResponse[tokenJSON]{Items: items})
}

type createTokenRequest struct {
	Project string `json:"project"`
	Scope   string `json:"scope"`
	Name    string `json:"name"`
}

type createTokenResponse struct {
	tokenJSON
	// Token is the plaintext, returned only here.
	Token string `json:"token"`
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, plaintext, err := h.projects.CreateToken(r.Context(), project.NewTokenParams{
		ProjectSlug: req.Project, Scope: project.Scope(req.Scope), Name: req.Name,
	})
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createTokenResponse{tokenJSON: toTokenJSON(t), Token: plaintext})
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	if err := h.projects.RevokeToken(r.Context(), project.TokenID(r.PathValue("id"))); err != nil {
		h.serviceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
