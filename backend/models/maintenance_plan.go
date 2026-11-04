package models

import (
	"encoding/json"
	"time"
)

const MaintenancePlanVersion = 1

// MaintenancePlanEnvelope is the operation-agnostic, Backend-owned plan record.
// Operation-specific details stay in Payload and are interpreted by later steps.
type MaintenancePlanEnvelope struct {
	Version           int                     `json:"version"`
	PlanID            string                  `json:"plan_id"`
	OperationType     string                  `json:"operation_type"`
	PayloadSchemaID   string                  `json:"payload_schema_id"`
	Payload           json.RawMessage         `json:"payload"`
	CreatedAt         time.Time               `json:"created_at"`
	ReadyToApply      bool                    `json:"ready_to_apply"`
	BackupRequirement string                  `json:"backup_requirement"`
	Actions           []MaintenancePlanAction `json:"actions"`
	Digest            string                  `json:"plan_digest,omitempty"`
}

// MaintenancePlanAction contains the shared selection and backup boundary.
// Operation-specific validation remains outside this common model.
type MaintenancePlanAction struct {
	ID                  string       `json:"action_id"`
	Action              string       `json:"action"`
	Selectable          bool         `json:"selectable"`
	Destructive         bool         `json:"destructive"`
	BackupSources       []string     `json:"backup_sources,omitempty"`
	BackupFiles         []BackupFile `json:"backup_files,omitempty"`
	BackupDirectories   []string     `json:"backup_directories,omitempty"`
	BackupInventoryHash string       `json:"backup_inventory_hash,omitempty"`
	BackupFileCount     int64        `json:"backup_file_count,omitempty"`
	BackupTotalSize     int64        `json:"backup_total_size,omitempty"`
}
