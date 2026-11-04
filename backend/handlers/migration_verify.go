package handlers

import (
	"errors"
	"net/http"
	"reflect"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const MigrationVerifyRoute = "/api/controller/maintenance/migration/verify"

type migrationVerifier interface {
	Verify(models.MigrationVerifyRequest) (models.MigrationVerifyResult, error)
}
type MigrationVerifyHandler struct{ service migrationVerifier }

func NewMigrationVerifyHandler(service migrationVerifier) *MigrationVerifyHandler {
	if service != nil {
		v := reflect.ValueOf(service)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			service = nil
		}
	}
	return &MigrationVerifyHandler{service: service}
}

func (h *MigrationVerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != MigrationVerifyRoute {
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
	request, ok := DecodeMaintenanceRequest[models.MigrationVerifyRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "migration verify unavailable")
		return
	}
	result, err := h.service.Verify(request)
	if err != nil {
		switch {
		case errors.Is(err, repositories.ErrMaintenanceMissing):
			writeMaintenanceInputError(w, 404, "migration operation or plan not found")
		case errors.Is(err, services.ErrMigrationVerifyInvalid), errors.Is(err, services.ErrMigrationPlanInvalid):
			writeMaintenanceInputError(w, 400, "invalid migration operation or plan")
		default:
			writeMaintenanceInputError(w, 503, "migration verify unavailable; inspect maintenance state")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, 200, struct {
		OK bool `json:"ok"`
		models.MigrationVerifyResult
	}{true, result})
}
