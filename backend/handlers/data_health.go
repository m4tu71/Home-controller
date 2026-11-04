package handlers

import (
	"net/http"

	"backend/models"
)

const DataHealthRoute = "/api/controller/maintenance/data-health"

type dataHealthScanner interface {
	Scan() ([]models.DataHealthIssue, error)
}

type DataHealthHandler struct{ service dataHealthScanner }

func NewDataHealthHandler(service dataHealthScanner) *DataHealthHandler {
	return &DataHealthHandler{service: service}
}

// Keep filesystem-absolute canonical paths internal to the Backend. The API
// exposes only root-relative paths and content hashes for plan selection.
type dataHealthIssueView struct {
	ID                    string `json:"id"`
	Severity              string `json:"severity"`
	Path                  string `json:"path"`
	Hash                  string `json:"hash"`
	Message               string `json:"message"`
	HasDestructiveActions bool   `json:"has_destructive_actions"`
}

func (h *DataHealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != DataHealthRoute {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	if r.Method != http.MethodGet {
		writeMaintenanceMethodNotAllowed(w, http.MethodGet)
		return
	}
	if r.URL.RawQuery != "" {
		writeMaintenanceInputError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "data health unavailable")
		return
	}
	issues, err := h.service.Scan()
	if err != nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "data health scan failed")
		return
	}
	views := make([]dataHealthIssueView, 0, len(issues))
	for _, issue := range issues {
		views = append(views, dataHealthIssueView{ID: issue.ID, Severity: issue.Severity, Path: issue.Path, Hash: issue.Hash, Message: issue.Message, HasDestructiveActions: issue.HasDestructiveActions})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, http.StatusOK, struct {
		OK     bool                  `json:"ok"`
		Issues []dataHealthIssueView `json:"issues"`
	}{OK: true, Issues: views})
}
