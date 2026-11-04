package models

// MaintenanceSnapshot is the generic read-only reconciliation shape carried
// by an action's before_state and intended_state. Paths are root-relative.
type MaintenanceSnapshot struct {
	Runtime *MaintenanceSnapshotEntry `json:"runtime,omitempty"`
	Trash   *MaintenanceSnapshotEntry `json:"trash,omitempty"`
}

type MaintenanceSnapshotEntry struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Kind   string `json:"kind,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}
