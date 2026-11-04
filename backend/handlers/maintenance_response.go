package handlers

import (
	"errors"
	"net/http"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

func writeMaintenanceMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeMaintenanceInputError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func maintenanceApplyErrorStatus(err error, invalidErrors ...error) int {
	switch {
	case errors.Is(err, repositories.ErrMaintenanceMissing),
		errors.Is(err, repositories.ErrBackupMissing),
		errors.Is(err, services.ErrMaintenancePlanActionMissing):
		return http.StatusNotFound
	case errors.Is(err, repositories.ErrMaintenanceID),
		errors.Is(err, services.ErrMaintenancePlanInvalid),
		errors.Is(err, services.ErrMigrationPlanInvalid):
		return http.StatusBadRequest
	}
	for _, invalid := range invalidErrors {
		if errors.Is(err, invalid) {
			return http.StatusBadRequest
		}
	}
	switch {
	case errors.Is(err, services.ErrMaintenancePlanStale),
		errors.Is(err, services.ErrMaintenancePlanNotReady),
		errors.Is(err, services.ErrMigrationApplyStale),
		errors.Is(err, services.ErrCleanupIssueStale),
		errors.Is(err, repositories.ErrMaintenancePlanClaimed),
		errors.Is(err, repositories.ErrMaintenanceTargetClaimed),
		errors.Is(err, repositories.ErrMaintenanceDuplicate),
		errors.Is(err, repositories.ErrMigrationWriteConflict),
		errors.Is(err, repositories.ErrBackupConflict),
		errors.Is(err, repositories.ErrTrashChanged),
		errors.Is(err, repositories.ErrTrashCrossDevice):
		return http.StatusConflict
	default:
		// Corrupt state, backup/readback failures, uncertain outcomes, and
		// infrastructure errors stay unavailable so clients inspect history.
		return http.StatusServiceUnavailable
	}
}

func writeMaintenanceOperationError(w http.ResponseWriter, status int, message string, operation models.MaintenanceOperation) {
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, status, struct {
		OK            bool   `json:"ok"`
		Error         string `json:"error"`
		OperationID   string `json:"operation_id,omitempty"`
		Status        string `json:"status,omitempty"`
		ApplyBackupID string `json:"apply_backup_id,omitempty"`
	}{false, message, operation.ID, operation.Status, operation.ApplyBackupID})
}
