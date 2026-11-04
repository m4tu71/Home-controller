package handlers

import (
	"errors"
	"net/http"
	"strings"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const backupPath = "/api/controller/maintenance/backups/"

type backupDetailReader interface {
	Detail(string) (models.BackupDetail, error)
}
type backupRestorer interface {
	RestoreDryRun(string) (repositories.BackupRestorePreview, error)
	RestoreRehearsal(string) (repositories.BackupRestoreResult, error)
}
type BackupDetailHandler struct {
	reader   backupDetailReader
	restorer backupRestorer
}
type BackupRestoreHandler struct{ restorer backupRestorer }

func NewBackupDetailHandler(reader *services.BackupService) *BackupDetailHandler {
	return &BackupDetailHandler{reader: reader, restorer: reader}
}

func NewBackupRestoreHandler(service *services.BackupService) *BackupRestoreHandler {
	return &BackupRestoreHandler{restorer: service}
}

func (h *BackupDetailHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, backupPath) {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, backupPath)
	parts := strings.Split(path, "/")
	isRestoreRoute := len(parts) == 3 && parts[0] != "" && parts[1] == "restore" && (parts[2] == "dry-run" || parts[2] == "rehearsal")
	if isRestoreRoute {
		if r.Method != http.MethodPost {
			writeMaintenanceMethodNotAllowed(w, http.MethodPost)
			return
		}
		if h == nil {
			writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup restore unavailable")
			return
		}
		(&BackupRestoreHandler{restorer: h.restorer}).ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodPost {
		if h == nil {
			writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup restore unavailable")
			return
		}
		(&BackupRestoreHandler{restorer: h.restorer}).ServeHTTP(w, r)
		return
	}
	id := path
	if id == "" || strings.Contains(id, "/") {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	if r.Method != http.MethodGet {
		writeMaintenanceMethodNotAllowed(w, http.MethodGet)
		return
	}
	if safeMaintenanceToken(id) != id {
		writeMaintenanceInputError(w, 400, "invalid backup ID")
		return
	}
	if h == nil || h.reader == nil {
		writeMaintenanceInputError(w, 503, "backup unavailable")
		return
	}
	detail, err := h.reader.Detail(id)
	switch {
	case errors.Is(err, repositories.ErrMaintenanceID):
		writeMaintenanceInputError(w, 400, "invalid backup ID")
	case errors.Is(err, repositories.ErrBackupMissing):
		writeMaintenanceInputError(w, 404, "backup not found")
	case err != nil:
		writeMaintenanceInputError(w, 503, "backup unavailable")
	default:
		b := detail.Bundle
		writeMaintenanceJSON(w, 200, map[string]any{"backup_id": detail.Registry.ID, "role": detail.Registry.Role, "state": detail.Registry.State, "created_at": b.CreatedAt, "plan_id": b.PlanID, "action_ids": b.ActionIDs, "sources": b.Sources, "file_count": b.FileCount, "total_size": b.TotalSize, "inventory_hash": b.InventoryHash, "files": b.Files})
	}
}

func (h *BackupRestoreHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, backupPath), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "restore" || (parts[2] != "dry-run" && parts[2] != "rehearsal") {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	id := parts[0]
	if safeMaintenanceToken(id) != id {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid backup ID")
		return
	}
	if !RequireEmptyMaintenanceBody(w, r, MaintenanceJSONBodyLimit256KiB) {
		return
	}
	if h == nil || h.restorer == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup restore unavailable")
		return
	}
	if parts[2] == "dry-run" {
		preview, err := h.restorer.RestoreDryRun(id)
		if backupRestoreError(w, err) {
			return
		}
		if preview.Reason == "wrong_backup_role" || preview.Reason == "backup_not_rehearsal_candidate" {
			writeMaintenanceInputError(w, http.StatusConflict, "backup is not an eligible rehearsal candidate")
			return
		}
		writeMaintenanceJSON(w, http.StatusOK, preview)
		return
	}
	result, err := h.restorer.RestoreRehearsal(id)
	if backupRestoreError(w, err) {
		return
	}
	writeMaintenanceJSON(w, http.StatusOK, result)
}

func backupRestoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, repositories.ErrMaintenanceID):
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid backup ID")
	case errors.Is(err, repositories.ErrBackupMissing):
		writeMaintenanceInputError(w, http.StatusNotFound, "backup not found")
	case errors.Is(err, repositories.ErrBackupRestoreConflict), errors.Is(err, repositories.ErrBackupConflict):
		writeMaintenanceInputError(w, http.StatusConflict, "backup restore conflict; inspect bundle and isolated target")
	default:
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "backup restore unavailable; inspect bundle and target")
	}
	return true
}
