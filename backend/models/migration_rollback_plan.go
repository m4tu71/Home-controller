package models

import "fmt"

type MigrationRollbackApplyRequest struct {
	PlanID     string   `json:"plan_id"`
	PlanDigest string   `json:"plan_digest"`
	ActionIDs  []string `json:"action_ids"`
	Confirm    bool     `json:"confirm"`
}

func (r MigrationRollbackApplyRequest) Validate() error {
	if r.PlanID == "" || r.PlanDigest == "" || !r.Confirm {
		return fmt.Errorf("plan_id, plan_digest and confirm=true are required")
	}
	return validateMaintenanceActionIDs(r.ActionIDs)
}

type MigrationRollbackDryRunRequest struct {
	OperationID string `json:"operation_id"`
}

func (r MigrationRollbackDryRunRequest) Validate() error {
	if r.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	return nil
}

type MigrationRollbackPlanPayload struct {
	MigrationOperationID string                    `json:"migration_operation_id"`
	MigrationPlanID      string                    `json:"migration_plan_id"`
	MigrationPlanDigest  string                    `json:"migration_plan_digest"`
	ApplyBackupID        string                    `json:"apply_backup_id"`
	Targets              []MigrationRollbackTarget `json:"targets"`
}

type MigrationRollbackTarget struct {
	ActionID           string                        `json:"action_id"`
	TargetPath         string                        `json:"target_path"`
	ApplySHA256        string                        `json:"apply_sha256"`
	CurrentExists      bool                          `json:"current_exists"`
	CurrentKind        string                        `json:"current_kind,omitempty"`
	CurrentSHA256      string                        `json:"current_sha256,omitempty"`
	Eligible           bool                          `json:"eligible"`
	Reason             string                        `json:"reason"`
	ExpectedAfterState string                        `json:"expected_after_state"`
	Dependencies       []MigrationRollbackDependency `json:"dependencies"`
}

type MigrationRollbackDependency struct {
	Path              string   `json:"path"`
	Kind              string   `json:"kind"`
	Role              string   `json:"role"`
	CreatedByApply    bool     `json:"created_by_apply"`
	SharedByActionIDs []string `json:"shared_by_action_ids"`
}
