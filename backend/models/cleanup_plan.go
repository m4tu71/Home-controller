package models

// CleanupIssueSelection is an explicit snapshot of a Data Health finding.
// Callers must supply both fields; a changed finding cannot be planned.
type CleanupIssueSelection struct {
	IssueID    string `json:"issue_id"`
	BeforeHash string `json:"before_hash"`
}

type CleanupPlanAction struct {
	IssueID            string   `json:"issue_id"`
	ActionID           string   `json:"action_id"`
	Action             string   `json:"action"`
	Destructive        bool     `json:"destructive"`
	AffectedPaths      []string `json:"affected_paths"`
	BeforeHash         string   `json:"before_hash"`
	ExpectedAfterState string   `json:"expected_after_state"`
}

type CleanupPlanPayload struct {
	Actions []CleanupPlanAction `json:"actions"`
}
