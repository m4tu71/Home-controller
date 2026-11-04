package models

import "time"

// TrashEntry describes one original runtime object and its private payload.
// A prepared entry may not yet have moved; sealed means readback succeeded.
type TrashEntry struct {
	Version                 int            `json:"version"`
	ID                      string         `json:"id"`
	Status                  string         `json:"status"`
	OriginalRelativePath    string         `json:"original_relative_path"`
	OriginalID              string         `json:"original_id"`
	TargetType              string         `json:"target_type"`
	Reason                  string         `json:"reason"`
	OperationType           string         `json:"operation_type"`
	OperationID             string         `json:"operation_id"`
	BeforeInventory         TrashInventory `json:"before_inventory"`
	DeletedAt               *time.Time     `json:"deleted_at,omitempty"`
	EligibleAfter           *time.Time     `json:"eligible_after,omitempty"`
	LastCheckedAt           *time.Time     `json:"last_checked_at,omitempty"`
	RestoreAvailable        bool           `json:"restore_available"`
	RestoreOperationID      string         `json:"restore_operation_id,omitempty"`
	RestoredAt              *time.Time     `json:"restored_at,omitempty"`
	FinalDeletePlanID       string         `json:"final_delete_plan_id,omitempty"`
	FinalDeleteOperationID  string         `json:"final_delete_operation_id,omitempty"`
	FinalDeleteFilesDeleted int64          `json:"final_delete_files_deleted,omitempty"`
	FinalDeleteFilesTotal   int64          `json:"final_delete_files_total,omitempty"`
	FinalDeletedAt          *time.Time     `json:"final_deleted_at,omitempty"`
}

type TrashInventory struct {
	Kind      string `json:"kind"`
	FileCount int64  `json:"file_count"`
	TotalSize int64  `json:"total_size"`
	SHA256    string `json:"sha256"`
}
