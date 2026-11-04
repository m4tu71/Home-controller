package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const MigrationRollbackDryRunRoute = "/api/controller/maintenance/migration/rollback/dry-run"

type migrationRollbackPlanner interface {
	Create(models.MigrationRollbackDryRunRequest) (models.MaintenancePlanEnvelope, error)
}
type MigrationRollbackDryRunHandler struct{ service migrationRollbackPlanner }

func NewMigrationRollbackDryRunHandler(service migrationRollbackPlanner) *MigrationRollbackDryRunHandler {
	if service != nil {
		v := reflect.ValueOf(service)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			service = nil
		}
	}
	return &MigrationRollbackDryRunHandler{service: service}
}

func (h *MigrationRollbackDryRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != MigrationRollbackDryRunRoute {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	if r.URL.RawQuery != "" {
		writeMaintenanceInputError(w, 400, "query parameters are not supported")
		return
	}
	request, ok := DecodeMaintenanceRequest[models.MigrationRollbackDryRunRequest](w, r, MaintenanceJSONBodyLimit1MiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "migration rollback planning unavailable")
		return
	}
	plan, err := h.service.Create(request)
	if err != nil {
		switch {
		case errors.Is(err, repositories.ErrMaintenanceMissing), errors.Is(err, repositories.ErrBackupMissing):
			writeMaintenanceInputError(w, 404, "migration operation, plan or backup not found")
		case errors.Is(err, services.ErrMigrationRollbackInvalid), errors.Is(err, services.ErrMigrationPlanInvalid):
			writeMaintenanceInputError(w, 400, "invalid migration rollback source")
		case errors.Is(err, services.ErrMaintenancePlanStale):
			writeMaintenanceInputError(w, 409, "migration target changed during planning")
		default:
			writeMaintenanceInputError(w, 503, "migration rollback planning unavailable; inspect maintenance state")
		}
		return
	}
	var payload models.MigrationRollbackPlanPayload
	if json.Unmarshal(plan.Payload, &payload) != nil {
		writeMaintenanceInputError(w, 503, "migration rollback plan invalid")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, 201, struct {
		OK      bool                             `json:"ok"`
		Plan    models.MaintenancePlanEnvelope   `json:"plan"`
		Targets []models.MigrationRollbackTarget `json:"targets"`
	}{true, plan, payload.Targets})
}
