package models

import "fmt"

type MigrationApplyRequest struct {
	PlanID            string   `json:"plan_id"`
	PlanDigest        string   `json:"plan_digest"`
	ActionIDs         []string `json:"action_ids"`
	RehearsalBackupID string   `json:"rehearsal_backup_id"`
	Confirm           bool     `json:"confirm"`
}

func (r MigrationApplyRequest) Validate() error {
	if r.PlanID == "" || r.PlanDigest == "" || r.RehearsalBackupID == "" {
		return fmt.Errorf("plan_id, plan_digest and rehearsal_backup_id are required")
	}
	if err := validateMaintenanceActionIDs(r.ActionIDs); err != nil {
		return err
	}
	if !r.Confirm {
		return fmt.Errorf("confirm must be true")
	}
	return nil
}
