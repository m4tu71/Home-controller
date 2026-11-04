package models

import (
	"encoding/json"
	"time"
)

// MaintenanceOperation is a durable, operation-agnostic progress record.
// Transition rules and action payload semantics belong to later steps.
type MaintenanceOperation struct {
	Version           int                          `json:"version"`
	ID                string                       `json:"id"`
	Revision          uint64                       `json:"revision"`
	StartedAt         time.Time                    `json:"started_at"`
	Target            string                       `json:"target"`
	PlanID            string                       `json:"plan_id,omitempty"`
	PlanDigest        string                       `json:"plan_digest,omitempty"`
	ClaimedPaths      []string                     `json:"claimed_paths,omitempty"`
	Type              string                       `json:"type,omitempty"`
	Actions           []MaintenanceOperationAction `json:"actions"`
	Events            []MaintenanceOperationEvent  `json:"events,omitempty"`
	RehearsalBackupID string                       `json:"rehearsal_backup_id,omitempty"`
	ApplyBackupID     string                       `json:"apply_backup_id,omitempty"`
	Status            string                       `json:"status"`
	Result            string                       `json:"result,omitempty"`
	Error             string                       `json:"error,omitempty"`
	CompletedAt       *time.Time                   `json:"completed_at,omitempty"`
	Recovery          *MaintenanceRecovery         `json:"recovery,omitempty"`
}

type MaintenanceRecovery struct {
	PreviousStatus string                      `json:"previous_status"`
	CheckedAt      time.Time                   `json:"checked_at"`
	Actions        []MaintenanceRecoveryAction `json:"actions"`
}

type MaintenanceRecoveryAction struct {
	ActionID       string `json:"action_id"`
	Classification string `json:"classification"`
	Reason         string `json:"reason,omitempty"`
}

type MaintenanceOperationAction struct {
	ID            string          `json:"id"`
	Action        string          `json:"action"`
	Status        string          `json:"status"`
	BeforeState   json.RawMessage `json:"before_state,omitempty"`
	IntendedState json.RawMessage `json:"intended_state,omitempty"`
	AfterState    json.RawMessage `json:"after_state,omitempty"`
	Error         string          `json:"error,omitempty"`
}

// Events are durable checkpoints, not an authenticated audit chain.
type MaintenanceOperationEvent struct {
	At            time.Time       `json:"at"`
	ActionID      string          `json:"action_id"`
	Kind          string          `json:"kind"`
	BeforeState   json.RawMessage `json:"before_state,omitempty"`
	IntendedState json.RawMessage `json:"intended_state,omitempty"`
	AfterState    json.RawMessage `json:"after_state,omitempty"`
	Error         string          `json:"error,omitempty"`
}
