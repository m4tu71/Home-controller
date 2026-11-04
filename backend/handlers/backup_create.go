package handlers

import (
	"errors"
	"net/http"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const BackupCreateRoute = "/api/controller/maintenance/backups"

type backupCreator interface {
	CreateRehearsal(models.BackupCreateRequest) (models.BackupDetail, error)
}

type BackupCreateHandler struct{ service backupCreator }

func NewBackupCreateHandler(service backupCreator) *BackupCreateHandler {
	return &BackupCreateHandler{service: service}
}

func (h *BackupCreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != BackupCreateRoute {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	request, ok := DecodeMaintenanceRequest[models.BackupCreateRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup creation unavailable")
		return
	}
	detail, err := h.service.CreateRehearsal(request)
	if err != nil {
		writeBackupCreateError(w, err)
		return
	}
	bundle := detail.Bundle
	writeMaintenanceJSON(w, http.StatusCreated, map[string]any{
		"backup_id":      detail.Registry.ID,
		"role":           detail.Registry.Role,
		"state":          detail.Registry.State,
		"plan_id":        bundle.PlanID,
		"action_ids":     bundle.ActionIDs,
		"file_count":     bundle.FileCount,
		"total_size":     bundle.TotalSize,
		"inventory_hash": bundle.InventoryHash,
	})
}

func writeBackupCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrMaintenanceMissing), errors.Is(err, services.ErrMaintenancePlanActionMissing), errors.Is(err, repositories.ErrBackupMissing):
		writeMaintenanceInputError(w, http.StatusNotFound, "maintenance plan or action not found")
	case errors.Is(err, services.ErrMaintenancePlanInvalid):
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid backup request")
	case errors.Is(err, services.ErrMaintenancePlanNotReady), errors.Is(err, services.ErrMaintenancePlanStale), errors.Is(err, repositories.ErrBackupConflict):
		writeMaintenanceInputError(w, http.StatusConflict, "plan or backup source changed; create a new plan")
	default:
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup creation unavailable; inspect maintenance state")
	}
}
