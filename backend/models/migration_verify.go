package models

import "fmt"

type MigrationVerifyRequest struct {
	OperationID string `json:"operation_id"`
}

func (r MigrationVerifyRequest) Validate() error {
	if r.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	return nil
}

type MigrationVerifyTarget struct {
	ActionID       string `json:"action_id"`
	TargetPath     string `json:"target_path"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
}

type MigrationVerifyResult struct {
	OperationID string                  `json:"operation_id"`
	PlanID      string                  `json:"plan_id"`
	Status      string                  `json:"status"`
	Targets     []MigrationVerifyTarget `json:"targets"`
}
