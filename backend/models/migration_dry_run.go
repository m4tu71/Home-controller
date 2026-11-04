package models

import "errors"

// MigrationDryRunRequest names legacy sources and explicit destination IDs.
// No caller-supplied filesystem snapshots or writes are accepted.
type MigrationDryRunRequest struct {
	Mappings []MigrationDryRunMapping `json:"mappings"`
}

type MigrationDryRunMapping struct {
	ActionID    string             `json:"action_id"`
	SourcePath  string             `json:"source_path"`
	TargetKind  string             `json:"target_kind"`
	RoomID      string             `json:"room_id"`
	DeviceID    string             `json:"device_id"`
	TargetID    string             `json:"target_id"`
	Decision    *MigrationDecision `json:"decision,omitempty"`
	Room        *Room              `json:"room,omitempty"`
	Device      *Device            `json:"device,omitempty"`
	IRConfig    *IRConfig          `json:"ir_config,omitempty"`
	StateSchema IRStateSchema      `json:"state_schema,omitempty"`
}

func (r MigrationDryRunRequest) Validate() error {
	if len(r.Mappings) == 0 || len(r.Mappings) > 100 {
		return errors.New("mappings must contain 1 to 100 entries")
	}
	for _, m := range r.Mappings {
		if m.ActionID == "" || m.SourcePath == "" || m.RoomID == "" || m.DeviceID == "" || m.TargetID == "" || (m.TargetKind != "command" && m.TargetKind != "state") {
			return errors.New("mapping requires action_id, source_path, target_kind and target IDs")
		}
	}
	return nil
}
