package handlers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"backend/models"
	"backend/services"
)

const MigrationRollbackApplyRoute = "/api/controller/maintenance/migration/rollback/apply"

type migrationRollbackApplier interface {
	Apply(models.MigrationRollbackApplyRequest) (models.MaintenanceOperation, error)
}
type MigrationRollbackApplyHandler struct{ service migrationRollbackApplier }

func NewMigrationRollbackApplyHandler(service migrationRollbackApplier) *MigrationRollbackApplyHandler {
	if service != nil {
		v := reflect.ValueOf(service)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			service = nil
		}
	}
	return &MigrationRollbackApplyHandler{service: service}
}

func (h *MigrationRollbackApplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != MigrationRollbackApplyRoute {
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
	request, ok := DecodeMaintenanceRequest[models.MigrationRollbackApplyRequest](w, r, MaintenanceJSONBodyLimit1MiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "migration rollback apply unavailable")
		return
	}
	op, err := h.service.Apply(request)
	if err != nil {
		status := maintenanceApplyErrorStatus(err, services.ErrMigrationRollbackInvalid)
		message := "migration rollback apply unavailable; inspect operation and maintenance state"
		switch status {
		case http.StatusNotFound:
			message = "rollback plan or action not found"
		case http.StatusBadRequest:
			message = "invalid rollback apply selection or source"
		case http.StatusConflict:
			message = "rollback plan, target, or operation changed; inspect and create a new plan"
		}
		writeMaintenanceOperationError(w, status, message, op)
		return
	}
	actions := make([]map[string]any, 0, len(op.Actions))
	for _, action := range op.Actions {
		var snap models.MaintenanceSnapshot
		_ = json.Unmarshal(action.IntendedState, &snap)
		entryID := ""
		if snap.Trash != nil {
			entryID = strings.TrimSuffix(snap.Trash.Path, "/payload")
		}
		actions = append(actions, map[string]any{"action_id": action.ID, "status": action.Status, "trash_entry_id": entryID})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, 200, map[string]any{"ok": true, "operation_id": op.ID, "status": op.Status, "result": op.Result, "apply_backup_id": op.ApplyBackupID, "actions": actions})
}
