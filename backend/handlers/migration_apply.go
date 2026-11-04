package handlers

import (
	"net/http"
	"reflect"

	"backend/models"
	"backend/services"
)

const MigrationApplyRoute = "/api/controller/maintenance/migration/apply"

type migrationApplier interface {
	Apply(models.MigrationApplyRequest) (models.MaintenanceOperation, error)
}
type MigrationApplyHandler struct{ service migrationApplier }

func NewMigrationApplyHandler(service migrationApplier) *MigrationApplyHandler {
	if service != nil {
		v := reflect.ValueOf(service)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			service = nil
		}
	}
	return &MigrationApplyHandler{service: service}
}

func (h *MigrationApplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != MigrationApplyRoute {
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
	request, ok := DecodeMaintenanceRequest[models.MigrationApplyRequest](w, r, MaintenanceJSONBodyLimit1MiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "migration apply unavailable")
		return
	}
	op, err := h.service.Apply(request)
	if err != nil {
		writeMigrationApplyError(w, op, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, 200, map[string]any{"ok": true, "operation_id": op.ID, "status": op.Status, "result": op.Result, "rehearsal_backup_id": op.RehearsalBackupID, "apply_backup_id": op.ApplyBackupID, "actions": op.Actions})
}

func writeMigrationApplyError(w http.ResponseWriter, op models.MaintenanceOperation, err error) {
	status := maintenanceApplyErrorStatus(err, services.ErrMigrationApplyInvalid)
	message := "migration apply unavailable; inspect operation and maintenance state"
	switch status {
	case http.StatusNotFound:
		message = "migration plan or action not found"
	case http.StatusBadRequest:
		message = "invalid migration apply request or plan"
	case http.StatusConflict:
		message = "migration source, target, backup, or operation changed"
	}
	writeMaintenanceOperationError(w, status, message, op)
}
