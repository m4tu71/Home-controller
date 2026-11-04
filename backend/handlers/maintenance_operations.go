package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const maintenanceOperationsPath = "/api/controller/maintenance/operations"

type maintenanceHistoryReader interface {
	List() ([]models.MaintenanceOperation, error)
	Detail(string) (models.MaintenanceOperation, error)
}

type MaintenanceOperationsHandler struct{ history maintenanceHistoryReader }

func NewMaintenanceOperationsHandler(history *services.MaintenanceHistoryService) *MaintenanceOperationsHandler {
	return &MaintenanceOperationsHandler{history: history}
}

type operationSummary struct {
	OperationID       string     `json:"operation_id"`
	PlanID            string     `json:"plan_id,omitempty"`
	Type              string     `json:"type,omitempty"`
	Target            string     `json:"target,omitempty"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	RehearsalBackupID string     `json:"rehearsal_backup_id,omitempty"`
	ApplyBackupID     string     `json:"apply_backup_id,omitempty"`
}

type operationActionView struct {
	ActionID       string `json:"action_id"`
	Action         string `json:"action,omitempty"`
	Status         string `json:"status"`
	Classification string `json:"classification,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Error          string `json:"error,omitempty"`
}

type operationDetail struct {
	operationSummary
	PreviousStatus       string                `json:"previous_status,omitempty"`
	Actions              []operationActionView `json:"actions"`
	Error                string                `json:"error,omitempty"`
	AvailableNextActions []string              `json:"available_next_actions"`
}

func (h *MaintenanceOperationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMaintenanceMethodNotAllowed(w, http.MethodGet)
		return
	}
	if h == nil || h.history == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "maintenance history unavailable")
		return
	}
	if r.URL.Path == maintenanceOperationsPath {
		records, err := h.history.List()
		if err != nil {
			writeMaintenanceInputError(w, http.StatusServiceUnavailable, "maintenance history unavailable")
			return
		}
		items := make([]operationSummary, 0, len(records))
		for _, record := range records {
			items = append(items, summaryOf(record))
		}
		writeMaintenanceJSON(w, http.StatusOK, map[string]any{"operations": items})
		return
	}
	if !strings.HasPrefix(r.URL.Path, maintenanceOperationsPath+"/") {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, maintenanceOperationsPath+"/")
	if id == "" || strings.Contains(id, "/") {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	record, err := h.history.Detail(id)
	if errors.Is(err, repositories.ErrMaintenanceID) {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid operation ID")
		return
	}
	if errors.Is(err, repositories.ErrMaintenanceMissing) {
		writeMaintenanceInputError(w, http.StatusNotFound, "operation not found")
		return
	}
	if err != nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "maintenance history unavailable")
		return
	}
	writeMaintenanceJSON(w, http.StatusOK, detailOf(record))
}

func summaryOf(v models.MaintenanceOperation) operationSummary {
	return operationSummary{OperationID: v.ID, PlanID: safeMaintenanceToken(v.PlanID), Type: safeMaintenanceToken(v.Type), Target: safeMaintenanceToken(v.Target), Status: safeMaintenanceToken(v.Status), StartedAt: v.StartedAt, CompletedAt: v.CompletedAt, RehearsalBackupID: safeMaintenanceToken(v.RehearsalBackupID), ApplyBackupID: safeMaintenanceToken(v.ApplyBackupID)}
}

func detailOf(v models.MaintenanceOperation) operationDetail {
	d := operationDetail{operationSummary: summaryOf(v), Actions: make([]operationActionView, 0, len(v.Actions)), AvailableNextActions: []string{}}
	if v.Error != "" {
		d.Error = safeMaintenanceError(v.Error)
	}
	classes := make(map[string]models.MaintenanceRecoveryAction)
	if v.Recovery != nil {
		d.PreviousStatus = safeMaintenanceToken(v.Recovery.PreviousStatus)
		for _, a := range v.Recovery.Actions {
			classes[a.ActionID] = a
		}
	}
	ambiguous := len(v.Actions) == 0 || v.Recovery == nil
	for _, a := range v.Actions {
		view := operationActionView{ActionID: safeMaintenanceToken(a.ID), Action: safeMaintenanceToken(a.Action), Status: safeMaintenanceToken(a.Status)}
		if a.Error != "" {
			view.Error = safeMaintenanceError(a.Error)
		}
		if c, ok := classes[a.ID]; ok {
			view.Classification = safeMaintenanceToken(c.Classification)
			view.Reason = safeMaintenanceToken(c.Reason)
			if c.Classification == services.MaintenanceAmbiguous {
				ambiguous = true
			}
		} else if v.Status == "recovery_required" {
			ambiguous = true
		}
		d.Actions = append(d.Actions, view)
	}
	if v.Status == "recovery_required" {
		if ambiguous {
			d.AvailableNextActions = []string{"manual_inspection"}
		} else {
			d.AvailableNextActions = []string{"verify", "restore", "new_dry_run"}
		}
	}
	return d
}

func safeMaintenanceToken(value string) string {
	if value == "" {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return ""
		}
	}
	return value
}

func safeMaintenanceError(value string) string {
	if len(value) > 256 || strings.ContainsAny(value, "/\\\r\n\x00") {
		return "recorded failure; inspect backend host for details"
	}
	return value
}

func writeMaintenanceJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
