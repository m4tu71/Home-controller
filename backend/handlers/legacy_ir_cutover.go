package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const LegacyIRCutoverDryRunRoute = "/api/controller/maintenance/legacy-ir-cutover/dry-run"
const LegacyIRCutoverApplyRoute = "/api/controller/maintenance/legacy-ir-cutover/apply"

type legacyIRCutoverPlanner interface {
	Create(models.LegacyIRCutoverDryRunRequest) (models.MaintenancePlanEnvelope, error)
}

type legacyIRCutoverApplier interface {
	Apply(models.LegacyIRCutoverApplyRequest) (models.MaintenanceOperation, error)
}

type LegacyIRCutoverDryRunHandler struct{ service legacyIRCutoverPlanner }
type LegacyIRCutoverApplyHandler struct{ service legacyIRCutoverApplier }

func NewLegacyIRCutoverDryRunHandler(service legacyIRCutoverPlanner) *LegacyIRCutoverDryRunHandler {
	return &LegacyIRCutoverDryRunHandler{service: service}
}

func NewLegacyIRCutoverApplyHandler(service legacyIRCutoverApplier) *LegacyIRCutoverApplyHandler {
	return &LegacyIRCutoverApplyHandler{service: service}
}

func (h *LegacyIRCutoverDryRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != LegacyIRCutoverDryRunRoute {
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
	request, ok := DecodeMaintenanceRequest[models.LegacyIRCutoverDryRunRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "legacy IR cutover planning unavailable")
		return
	}
	plan, err := h.service.Create(request)
	if err != nil {
		writeLegacyIRCutoverError(w, models.MaintenanceOperation{}, err)
		return
	}
	a := plan.Actions[0]
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "plan_id": plan.PlanID, "plan_digest": plan.Digest,
		"ready_to_apply": plan.ReadyToApply, "backup_requirement": plan.BackupRequirement,
		"action_id": a.ID, "source_path": "data/ir-signals", "source_file_count": a.BackupFileCount,
		"source_total_size": a.BackupTotalSize, "source_inventory_hash": a.BackupInventoryHash,
		"files":   a.BackupFiles,
		"sources": request.Sources, "migration_operation_id": request.MigrationOperationID,
	})
}

func (h *LegacyIRCutoverApplyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != LegacyIRCutoverApplyRoute {
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
	request, ok := DecodeMaintenanceRequest[models.LegacyIRCutoverApplyRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "legacy IR cutover apply unavailable")
		return
	}
	op, err := h.service.Apply(request)
	if err != nil {
		writeLegacyIRCutoverError(w, op, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, http.StatusOK, map[string]any{
		"ok": true, "operation_id": op.ID, "status": op.Status, "result": op.Result,
		"rehearsal_backup_id": op.RehearsalBackupID, "apply_backup_id": op.ApplyBackupID,
		"action_id": legacyCutoverResponseActionID(op), "trash_entry_id": legacyCutoverTrashEntryID(op),
	})
}

func legacyCutoverResponseActionID(op models.MaintenanceOperation) string {
	if len(op.Actions) == 1 {
		return op.Actions[0].ID
	}
	return ""
}

func legacyCutoverTrashEntryID(op models.MaintenanceOperation) string {
	if len(op.Actions) != 1 {
		return ""
	}
	var intended models.MaintenanceSnapshot
	if json.Unmarshal(op.Actions[0].IntendedState, &intended) != nil || intended.Trash == nil {
		return ""
	}
	return strings.TrimSuffix(intended.Trash.Path, "/payload")
}

func writeLegacyIRCutoverError(w http.ResponseWriter, op models.MaintenanceOperation, err error) {
	status := http.StatusServiceUnavailable
	message := "legacy IR cutover unavailable; inspect operation and maintenance state"
	switch {
	case errors.Is(err, services.ErrLegacyIRCutoverInvalid), errors.Is(err, services.ErrMaintenancePlanInvalid):
		status, message = http.StatusBadRequest, "invalid legacy IR cutover request"
	case errors.Is(err, repositories.ErrMaintenanceMissing), errors.Is(err, services.ErrMaintenancePlanActionMissing):
		status, message = http.StatusNotFound, "cutover plan or migration operation not found"
	case errors.Is(err, services.ErrLegacyIRCutoverStale), errors.Is(err, services.ErrMaintenancePlanStale), errors.Is(err, repositories.ErrMaintenancePlanClaimed), errors.Is(err, repositories.ErrBackupConflict), errors.Is(err, services.ErrMaintenancePlanNotReady):
		status, message = http.StatusConflict, "legacy IR cutover source, backup, or operation changed"
	}
	writeMaintenanceOperationError(w, status, message, op)
}
