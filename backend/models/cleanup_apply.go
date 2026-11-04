package models

import "fmt"

type CleanupApplyRequest struct {
	PlanID     string                  `json:"plan_id"`
	PlanDigest string                  `json:"plan_digest"`
	Issues     []CleanupIssueSelection `json:"issues"`
	ActionIDs  []string                `json:"action_ids"`
	Confirm    bool                    `json:"confirm"`
}

func (r CleanupApplyRequest) Validate() error {
	if r.PlanID == "" || r.PlanDigest == "" {
		return fmt.Errorf("plan_id and plan_digest are required")
	}
	if err := (CleanupDryRunRequest{Issues: r.Issues}).Validate(); err != nil {
		return err
	}
	if err := validateMaintenanceActionIDs(r.ActionIDs); err != nil {
		return err
	}
	if !r.Confirm {
		return fmt.Errorf("confirm must be true")
	}
	return nil
}
