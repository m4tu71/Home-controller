package models

import "fmt"

type LegacyIRSourceClassification struct {
	Path           string `json:"path"`
	Classification string `json:"classification"`
}

type LegacyIRCutoverDryRunRequest struct {
	MigrationOperationID string                         `json:"migration_operation_id"`
	Sources              []LegacyIRSourceClassification `json:"sources"`
}

func (r LegacyIRCutoverDryRunRequest) Validate() error {
	if r.MigrationOperationID == "" || len(r.Sources) == 0 {
		return fmt.Errorf("migration_operation_id and sources are required")
	}
	for _, source := range r.Sources {
		if source.Path == "" || (source.Classification != "intentionally_skipped" && source.Classification != "invalid") {
			return fmt.Errorf("invalid source classification")
		}
	}
	return nil
}

type LegacyIRCutoverPlanPayload struct {
	MigrationOperationID string                         `json:"migration_operation_id"`
	SourcePath           string                         `json:"source_path"`
	Sources              []LegacyIRSourceClassification `json:"sources"`
}

type LegacyIRCutoverApplyRequest struct {
	PlanID            string `json:"plan_id"`
	PlanDigest        string `json:"plan_digest"`
	RehearsalBackupID string `json:"rehearsal_backup_id"`
	ConfirmSourcePath string `json:"confirm_source_path"`
	Confirm           bool   `json:"confirm"`
}

func (r LegacyIRCutoverApplyRequest) Validate() error {
	if r.PlanID == "" || r.PlanDigest == "" || r.RehearsalBackupID == "" || r.ConfirmSourcePath != "data/ir-signals" || !r.Confirm {
		return fmt.Errorf("plan_id, plan_digest, rehearsal_backup_id, exact confirm_source_path and confirm=true are required")
	}
	return nil
}
