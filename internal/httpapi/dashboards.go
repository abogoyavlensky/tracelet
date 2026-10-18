package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/dashboard"
)

// Dashboards is what the dashboard endpoints call.
type Dashboards interface {
	List(ctx context.Context, projectID string) ([]dashboard.Summary, error)
	Get(ctx context.Context, projectID, uid string) (dashboard.Dashboard, error)
	Put(ctx context.Context, projectID, uid string, def dashboard.Definition, expect *int) (dashboard.Dashboard, bool, error)
	Delete(ctx context.Context, projectID, uid string) error
}

type dashboardJSON struct {
	UID       string    `json:"uid"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	dashboard.Definition
}

func toDashboardJSON(d dashboard.Dashboard) dashboardJSON {
	out := dashboardJSON{UID: d.UID, Revision: d.Revision, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Definition: d.Definition}
	if out.Panels == nil {
		out.Panels = []dashboard.Panel{}
	}
	return out
}

type dashboardSummaryJSON struct {
	UID       string    `json:"uid"`
	Title     string    `json:"title"`
	Panels    int       `json:"panels"`
	Revision  int       `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// putDashboardRequest is a dashboard as GET returns it, so it can be sent
// back as is: the timestamps are accepted and ignored.
type putDashboardRequest struct {
	UID       string          `json:"uid"`
	Revision  *int            `json:"revision"`
	CreatedAt json.RawMessage `json:"created_at"`
	UpdatedAt json.RawMessage `json:"updated_at"`
	dashboard.Definition
}

func (h *Handler) listDashboards(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProject(w, r, r.URL.Query().Get("project"))
	if !ok {
		return
	}
	list, err := h.dashboards.List(r.Context(), projectID)
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	items := make([]dashboardSummaryJSON, 0, len(list))
	for _, s := range list {
		items = append(items, dashboardSummaryJSON{UID: s.UID, Title: s.Title, Panels: s.Panels, Revision: s.Revision, UpdatedAt: s.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, listResponse[dashboardSummaryJSON]{Items: items})
}

func (h *Handler) getDashboard(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProject(w, r, r.URL.Query().Get("project"))
	if !ok {
		return
	}
	d, err := h.dashboards.Get(r.Context(), projectID, r.PathValue("uid"))
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDashboardJSON(d))
}

func (h *Handler) putDashboard(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProject(w, r, r.URL.Query().Get("project"))
	if !ok {
		return
	}
	var req putDashboardRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := r.PathValue("uid")
	if req.UID != "" && req.UID != uid {
		writeError(w, http.StatusBadRequest, "uid "+req.UID+" in the body does not match "+uid+" in the path")
		return
	}
	d, created, err := h.dashboards.Put(r.Context(), projectID, uid, req.Definition, req.Revision)
	if err != nil {
		h.serviceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toDashboardJSON(d))
}

func (h *Handler) deleteDashboard(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.resolveProject(w, r, r.URL.Query().Get("project"))
	if !ok {
		return
	}
	if err := h.dashboards.Delete(r.Context(), projectID, r.PathValue("uid")); err != nil {
		h.serviceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
