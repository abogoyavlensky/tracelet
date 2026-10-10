package httpapi

import (
	"net/http"

	"github.com/abogoyavlensky/tracelet/api"
)

func (h *Handler) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(api.OpenAPI)
}
