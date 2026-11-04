package services

import (
	"encoding/json"
	"path"
	"reflect"

	"backend/models"
)

const bedroomPresetSourceKind = "builtin_bedroom_preset"

type bedroomPresetSpec struct{ ID, Name, Pico string }

var bedroomPresetSpecs = []bedroomPresetSpec{
	{"on_ch3", "ON_CH3", "LIGHT_ON_CH3"},
	{"full_ch3", "FULL_CH3", "LIGHT_FULL_CH3"},
	{"off_ch3", "OFF_CH3", "LIGHT_OFF_CH3"},
	{"night_ch3", "NIGHT_CH3", "LIGHT_NIGHT_CH3"},
	{"bright_up_ch3", "BRIGHT_UP_CH3", "LIGHT_BRIGHT_UP_CH3"},
	{"bright_down_ch3", "BRIGHT_DOWN_CH3", "LIGHT_BRIGHT_DOWN_CH3"},
}

func bedroomPresetSpecFor(id string) (bedroomPresetSpec, bool) {
	for _, spec := range bedroomPresetSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return bedroomPresetSpec{}, false
}

func validBedroomPresetItem(item models.MigrationPlanAction) bool {
	if item.SourceKind != bedroomPresetSourceKind || item.Source != (models.MigrationPathSnapshot{}) || item.SourceFormat != "preset_command" || item.TargetFormat != models.OperationFormatPresetCommand || item.Transform != "preserve_preset_command" || item.Action != "write_ir_command" || item.ExpectedAfterState != "target_created_no_source" || item.Decision == nil {
		return false
	}
	kind, roomID, deviceID, targetID, ok := migrationTarget(item.TargetBefore.Path)
	spec, exists := bedroomPresetSpecFor(targetID)
	if !ok || !exists || kind != "command" || roomID != "bedroom" || deviceID != "roomlight" || item.ActionID != "roomlight_"+spec.ID || item.Decision.RoomID != roomID || item.Decision.DeviceID != deviceID || item.Decision.TargetID != targetID || item.Decision.TargetName != spec.Name || item.Decision.PicoCommand != spec.Pico || len(item.Decision.State) != 0 || item.Decision.ApproveRawMedian || item.TargetParentBefore.Path != path.Dir(item.TargetBefore.Path) {
		return false
	}
	base := "rooms/bedroom/devices/roomlight/adapters/ir"
	want := map[string]struct {
		kind, role string
		value      any
	}{
		"rooms/bedroom":                            {"directory", "directory", nil},
		"rooms/bedroom/devices":                    {"directory", "directory", nil},
		"rooms/bedroom/devices/roomlight":          {"directory", "directory", nil},
		"rooms/bedroom/devices/roomlight/adapters": {"directory", "directory", nil},
		base:                        {"directory", "directory", nil},
		path.Join(base, "commands"): {"directory", "directory", nil},
		"rooms/bedroom/room.json":   {"json", "room", models.Room{ID: "bedroom", Name: "Bedroom"}},
		"rooms/bedroom/devices/roomlight/device.json": {"json", "device", models.Device{ID: "roomlight", Name: "Roomlight", RoomID: "bedroom", DeviceType: "light", PrimaryAdapter: "ir"}},
		path.Join(base, "ir_config.json"):             {"json", "ir_config", models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38, DefaultRepeat: 1}},
		item.TargetBefore.Path:                        {"json", "operation", nil},
	}
	if item.TargetParentBefore.Exists {
		if item.TargetParentBefore.Kind != "directory" || item.TargetParentBefore.SHA256 == "" || len(item.PlannedWrites) != 1 {
			return false
		}
		w := item.PlannedWrites[0]
		return w.Path == item.TargetBefore.Path && w.Kind == "json" && w.Role == "operation" && w.Before == item.TargetBefore && w.ExpectedAfterState == "created" && len(w.Content) == 0
	}
	if len(item.PlannedWrites) != len(want) {
		return false
	}
	for _, w := range item.PlannedWrites {
		expected, exists := want[w.Path]
		if !exists || expected.kind != w.Kind || expected.role != w.Role {
			return false
		}
		if expected.value == nil {
			if len(w.Content) != 0 {
				return false
			}
			continue
		}
		var actual any
		switch expected.value.(type) {
		case models.Room:
			actual = new(models.Room)
		case models.Device:
			actual = new(models.Device)
		case models.IRConfig:
			actual = new(models.IRConfig)
		}
		if json.Unmarshal(w.Content, actual) != nil || !reflect.DeepEqual(reflect.ValueOf(actual).Elem().Interface(), expected.value) {
			return false
		}
	}
	return true
}
