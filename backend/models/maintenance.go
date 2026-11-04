package models

import (
	"fmt"
	"strings"
)

// MaintenanceErrorResponse is the stable JSON error shape for maintenance
// HTTP endpoints. It intentionally matches the existing controller API error
// envelope.
type MaintenanceErrorResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// MaintenanceApplyRequest is the common plan/action/confirmation envelope
// used by destructive maintenance apply requests.
type MaintenanceApplyRequest struct {
	PlanID     string   `json:"plan_id"`
	PlanDigest string   `json:"plan_digest"`
	ActionIDs  []string `json:"action_ids"`
	Confirm    bool     `json:"confirm"`
}

func (r MaintenanceApplyRequest) Validate() error {
	if strings.TrimSpace(r.PlanID) == "" {
		return fmt.Errorf("plan_id is required")
	}
	if strings.TrimSpace(r.PlanDigest) == "" {
		return fmt.Errorf("plan_digest is required")
	}
	if err := validateMaintenanceActionIDs(r.ActionIDs); err != nil {
		return err
	}
	if !r.Confirm {
		return fmt.Errorf("confirm must be true")
	}
	return nil
}

// BackupCreateRequest is deliberately narrower than an apply request: backup
// creation selects actions from a saved plan but does not accept a confirmation
// flag, paths, destinations, or a caller-selected backup role.
type BackupCreateRequest struct {
	PlanID     string   `json:"plan_id"`
	PlanDigest string   `json:"plan_digest"`
	ActionIDs  []string `json:"action_ids"`
}

func (r BackupCreateRequest) Validate() error {
	if strings.TrimSpace(r.PlanID) == "" {
		return fmt.Errorf("plan_id is required")
	}
	if strings.TrimSpace(r.PlanDigest) == "" {
		return fmt.Errorf("plan_digest is required")
	}
	return validateMaintenanceActionIDs(r.ActionIDs)
}

// TrashRestoreRequest confirms restoration of the inventory shown by the
// preceding read-only dry-run. Paths and IDs are intentionally not request
// fields; they are resolved from the Trash entry.
type TrashRestoreRequest struct {
	ExpectedInventoryHash string `json:"expected_inventory_hash"`
	Confirm               bool   `json:"confirm"`
}

func (r TrashRestoreRequest) Validate() error {
	if strings.TrimSpace(r.ExpectedInventoryHash) == "" {
		return fmt.Errorf("expected_inventory_hash is required")
	}
	if !r.Confirm {
		return fmt.Errorf("confirm must be true")
	}
	return nil
}

func validateMaintenanceActionIDs(actionIDs []string) error {
	if len(actionIDs) == 0 {
		return fmt.Errorf("action_ids must contain at least one action")
	}

	seen := make(map[string]struct{}, len(actionIDs))
	for _, actionID := range actionIDs {
		if strings.TrimSpace(actionID) == "" {
			return fmt.Errorf("action_ids must not contain empty values")
		}
		if _, exists := seen[actionID]; exists {
			return fmt.Errorf("action_ids must not contain duplicates")
		}
		seen[actionID] = struct{}{}
	}
	return nil
}
