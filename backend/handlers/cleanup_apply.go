package handlers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"

	"backend/models"
	"backend/services"
)

const CleanupApplyRoute = "/api/controller/maintenance/data-cleanup/apply"

type cleanupApplier interface {
	Apply(models.CleanupApplyRequest) (models.MaintenanceOperation, error)
}

type CleanupApplyHandler struct{ service cleanupApplier }

func NewCleanupApplyHandler(service cleanupApplier) *CleanupApplyHandler {
	if service != nil {
		value := reflect.ValueOf(service)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			service = nil
		}
	}
	return &CleanupApplyHandler{service: service}
}

func (h *CleanupApplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != CleanupApplyRoute {
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
	request, ok := DecodeMaintenanceRequest[models.CleanupApplyRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "cleanup apply unavailable")
		return
	}
	op, err := h.service.Apply(request)
	if err != nil {
		writeCleanupApplyError(w, op, err)
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
	writeMaintenanceJSON(w, http.StatusOK, map[string]any{"ok": true, "operation_id": op.ID, "status": op.Status, "result": op.Result, "apply_backup_id": op.ApplyBackupID, "actions": actions})
}

func writeCleanupApplyError(w http.ResponseWriter, op models.MaintenanceOperation, err error) {
	status := maintenanceApplyErrorStatus(err, services.ErrCleanupSelectionInvalid)
	message := "cleanup apply unavailable; inspect operation and maintenance state"
	switch status {
	case http.StatusNotFound:
		message = "cleanup plan or action not found"
	case http.StatusBadRequest:
		message = "invalid cleanup apply selection"
	case http.StatusConflict:
		message = "cleanup plan, target, or operation changed; inspect and retry with a new plan"
	}
	writeMaintenanceOperationError(w, status, message, op)
}
