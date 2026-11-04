package handlers

import (
	"errors"
	"net/http"

	"backend/models"
	"backend/services"
)

const MigrationDryRunRoute = "/api/controller/maintenance/migration/dry-run"

type migrationDryRunCreator interface {
	Create(models.MigrationDryRunRequest) (models.MaintenancePlanEnvelope, error)
}

type MigrationDryRunHandler struct{ service migrationDryRunCreator }

func NewMigrationDryRunHandler(service migrationDryRunCreator) *MigrationDryRunHandler {
	return &MigrationDryRunHandler{service: service}
}

func (h *MigrationDryRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != MigrationDryRunRoute {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	if r.URL.RawQuery != "" {
		writeMaintenanceInputError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	request, ok := DecodeMaintenanceRequest[models.MigrationDryRunRequest](w, r, MaintenanceJSONBodyLimit1MiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "migration planning unavailable")
		return
	}
	plan, err := h.service.Create(request)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMigrationDryRunInvalid), errors.Is(err, services.ErrMigrationPlanInvalid):
			writeMaintenanceInputError(w, http.StatusBadRequest, "invalid migration mapping or source")
		case errors.Is(err, services.ErrMigrationDryRunConflict), errors.Is(err, services.ErrMaintenancePlanStale):
			writeMaintenanceInputError(w, http.StatusConflict, "migration source or target changed")
		default:
			writeMaintenanceInputError(w, http.StatusServiceUnavailable, "migration planning unavailable")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, http.StatusCreated, struct {
		OK   bool                           `json:"ok"`
		Plan models.MaintenancePlanEnvelope `json:"plan"`
	}{true, plan})
}
