package models

import "encoding/json"

// MigrationPlanPayload carries only source/target observations and conversion
// rules. RAW timings and captures remain in the source and are re-read later.
type MigrationPlanPayload struct {
	Actions []MigrationPlanAction `json:"actions"`
}

type MigrationPathSnapshot struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Kind   string `json:"kind,omitempty"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type MigrationPlanAction struct {
	ActionID           string                  `json:"action_id"`
	Action             string                  `json:"action"`
	SourceKind         string                  `json:"source_kind,omitempty"`
	Source             MigrationPathSnapshot   `json:"source"`
	SourceFormat       string                  `json:"source_format"`
	TargetBefore       MigrationPathSnapshot   `json:"target_before"`
	TargetParentBefore MigrationPathSnapshot   `json:"target_parent_before"`
	PlannedWrites      []MigrationPlannedWrite `json:"planned_writes"`
	TargetFormat       OperationFormat         `json:"target_format"`
	Transform          string                  `json:"transform"`
	ExpectedAfterState string                  `json:"expected_after_state"`
	Decision           *MigrationDecision      `json:"decision,omitempty"`
}

type MigrationPlannedWrite struct {
	Path               string                `json:"path"`
	Kind               string                `json:"kind"` // directory or json
	Role               string                `json:"role"` // directory, room, device, ir_config, state_schema, operation
	Before             MigrationPathSnapshot `json:"before"`
	ExpectedAfterState string                `json:"expected_after_state"`
	Content            json.RawMessage       `json:"content,omitempty"` // small non-RAW JSON only
}

type MigrationDecision struct {
	RoomID           string         `json:"room_id"`
	DeviceID         string         `json:"device_id"`
	TargetID         string         `json:"target_id"`
	TargetName       string         `json:"target_name,omitempty"`
	State            map[string]any `json:"state,omitempty"`
	ApproveRawMedian bool           `json:"approve_raw_median,omitempty"`
	PicoCommand      string         `json:"pico_command,omitempty"`
}
