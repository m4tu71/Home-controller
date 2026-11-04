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

const CleanupDryRunRoute = "/api/controller/maintenance/data-cleanup/dry-run"

type cleanupPlanCreator interface {
	Create([]models.CleanupIssueSelection) (models.MaintenancePlanEnvelope, error)
}

type CleanupDryRunHandler struct{ service cleanupPlanCreator }

func NewCleanupDryRunHandler(service cleanupPlanCreator) *CleanupDryRunHandler {
	if service != nil {
		value := reflect.ValueOf(service)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			service = nil
		}
	}
	return &CleanupDryRunHandler{service: service}
}

type cleanupDryRunActionView struct {
	IssueID             string   `json:"issue_id"`
	ActionID            string   `json:"action_id"`
	Action              string   `json:"action"`
	Destructive         bool     `json:"destructive"`
	AffectedPaths       []string `json:"affected_paths"`
	BeforeHash          string   `json:"before_hash"`
	ExpectedAfterState  string   `json:"expected_after_state"`
	BackupSources       []string `json:"backup_sources"`
	BackupInventoryHash string   `json:"backup_inventory_hash"`
	BackupFileCount     int64    `json:"backup_file_count"`
	BackupTotalSize     int64    `json:"backup_total_size"`
}

func (h *CleanupDryRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != CleanupDryRunRoute {
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
	request, ok := DecodeMaintenanceRequest[models.CleanupDryRunRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "cleanup planning unavailable")
		return
	}
	plan, err := h.service.Create(request.Issues)
	if err != nil {
		writeCleanupDryRunError(w, err)
		return
	}
	if err := services.ValidateCleanupPlan(plan); err != nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "cleanup plan invalid")
		return
	}
	var payload models.CleanupPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "cleanup plan invalid")
		return
	}
	byID := make(map[string]models.MaintenancePlanAction, len(plan.Actions))
	for _, action := range plan.Actions {
		byID[action.ID] = action
	}
	views := make([]cleanupDryRunActionView, 0, len(payload.Actions))
	for _, item := range payload.Actions {
		common := byID[item.ActionID]
		paths := make([]string, 0, len(item.AffectedPaths))
		for _, p := range item.AffectedPaths {
			paths = append(paths, "data/"+p)
		}
		sources := make([]string, 0, len(common.BackupSources))
		for _, p := range common.BackupSources {
			sources = append(sources, "data/"+p)
		}
		views = append(views, cleanupDryRunActionView{IssueID: item.IssueID, ActionID: item.ActionID, Action: item.Action, Destructive: item.Destructive, AffectedPaths: paths, BeforeHash: item.BeforeHash, ExpectedAfterState: item.ExpectedAfterState, BackupSources: sources, BackupInventoryHash: common.BackupInventoryHash, BackupFileCount: common.BackupFileCount, BackupTotalSize: common.BackupTotalSize})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, http.StatusCreated, struct {
		OK                bool                      `json:"ok"`
		PlanID            string                    `json:"plan_id"`
		PlanDigest        string                    `json:"plan_digest"`
		ReadyToApply      bool                      `json:"ready_to_apply"`
		BackupRequirement string                    `json:"backup_requirement"`
		BackupPlanned     bool                      `json:"backup_planned"`
		Actions           []cleanupDryRunActionView `json:"actions"`
	}{OK: true, PlanID: plan.PlanID, PlanDigest: plan.Digest, ReadyToApply: plan.ReadyToApply, BackupRequirement: plan.BackupRequirement, BackupPlanned: plan.BackupRequirement != "none", Actions: views})
}

func writeCleanupDryRunError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrCleanupSelectionInvalid), errors.Is(err, services.ErrCleanupIssueUnsupported), errors.Is(err, services.ErrMaintenancePlanInvalid):
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid cleanup issue selection")
	case errors.Is(err, services.ErrCleanupIssueMissing):
		writeMaintenanceInputError(w, http.StatusNotFound, "cleanup issue not found")
	case errors.Is(err, services.ErrCleanupIssueStale), errors.Is(err, services.ErrMaintenancePlanStale), errors.Is(err, repositories.ErrBackupConflict):
		writeMaintenanceInputError(w, http.StatusConflict, "cleanup target changed; scan again")
	default:
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "cleanup planning unavailable; inspect maintenance state")
	}
}
