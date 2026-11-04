package models

import "time"

// BackupBundle is the immutable inventory sealed after source/snapshot readback.
type BackupBundle struct {
	Version       int          `json:"version"`
	ID            string       `json:"id"`
	Role          string       `json:"role"`
	PlanID        string       `json:"plan_id,omitempty"`
	ActionIDs     []string     `json:"action_ids"`
	CreatedAt     time.Time    `json:"created_at"`
	Sources       []string     `json:"sources"`
	Files         []BackupFile `json:"files"`
	Directories   []string     `json:"directories"`
	FileCount     int64        `json:"file_count"`
	TotalSize     int64        `json:"total_size"`
	InventoryHash string       `json:"inventory_hash"`
}

type BackupFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	JSONStatus string `json:"json_status,omitempty"` // valid, invalid, or empty for .json files
}

// State is kept separately so future rehearsal promotion does not rewrite
// the sealed bundle inventory.
type BackupRegistry struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BackupDetail struct {
	Bundle   BackupBundle   `json:"bundle"`
	Registry BackupRegistry `json:"registry"`
}
