package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iradapter "backend/adapters/ir"
	"backend/models"
	"backend/repositories"
)

// These synthetic legacy files exist only below t.TempDir. The actual bedroom
// shortcut commands have no ir-signals/*.json source and are never fabricated
// in production by this fixture.
var bedroomLightPresets = []struct{ action, id, name, pico string }{
	{"on", "on_ch3", "ON_CH3", "LIGHT_ON_CH3"},
	{"full", "full_ch3", "FULL_CH3", "LIGHT_FULL_CH3"},
	{"off", "off_ch3", "OFF_CH3", "LIGHT_OFF_CH3"},
	{"night", "night_ch3", "NIGHT_CH3", "LIGHT_NIGHT_CH3"},
	{"bright_up", "bright_up_ch3", "BRIGHT_UP_CH3", "LIGHT_BRIGHT_UP_CH3"},
	{"bright_down", "bright_down_ch3", "BRIGHT_DOWN_CH3", "LIGHT_BRIGHT_DOWN_CH3"},
}

func bedroomLightFixture(t *testing.T, root string) models.MigrationDryRunRequest {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "ir-signals"), 0700); err != nil {
		t.Fatal(err)
	}
	request := models.MigrationDryRunRequest{Mappings: make([]models.MigrationDryRunMapping, 0, len(bedroomLightPresets))}
	for _, preset := range bedroomLightPresets {
		source := "ir-signals/" + preset.name + ".json"
		signal := models.MigrationSourceSignal{Name: preset.name, Room: "bedroom", Device: "roomlight", Format: "preset_command", CarrierKHz: 38}
		data, err := json.Marshal(signal)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(source)), data, 0600); err != nil {
			t.Fatal(err)
		}
		request.Mappings = append(request.Mappings, models.MigrationDryRunMapping{
			ActionID: "roomlight_" + preset.id, SourcePath: source, TargetKind: "command", RoomID: "bedroom", DeviceID: "roomlight", TargetID: preset.id,
			Decision: &models.MigrationDecision{RoomID: "bedroom", DeviceID: "roomlight", TargetID: preset.id, PicoCommand: preset.pico},
			Room:     &models.Room{ID: "bedroom", Name: "Bedroom"},
			Device:   &models.Device{ID: "roomlight", Name: "Roomlight", RoomID: "bedroom", DeviceType: "light", PrimaryAdapter: "ir"},
			IRConfig: &models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38, DefaultRepeat: 1},
		})
	}
	return request
}

func TestBedroomLightPresetFixturePlanOperationAndVerify(t *testing.T) {
	f := newCleanupApplyFixture(t)
	request := bedroomLightFixture(t, f.root)
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	plans := NewMigrationPlanService(common)
	plan, err := NewMigrationDryRunService(f.root, plans).Create(request)
	if err != nil || !plan.ReadyToApply || len(plan.Actions) != 6 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	var payload models.MigrationPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for _, item := range payload.Actions {
		if item.SourceFormat != "preset_command" || item.TargetFormat != models.OperationFormatPresetCommand || item.Transform != "preserve_preset_command" || item.Decision == nil || item.Decision.PicoCommand == "" || !strings.HasPrefix(item.TargetBefore.Path, "rooms/bedroom/devices/roomlight/adapters/ir/commands/") {
			t.Fatalf("mapping=%+v", item)
		}
	}
	for _, rel := range []string{"rooms/bedroom/room.json", "rooms/bedroom/devices/roomlight/device.json", "rooms/bedroom/devices/roomlight/adapters/ir/ir_config.json"} {
		if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote %s: %v", rel, err)
		}
	}
	ids := make([]string, 0, 6)
	for _, mapping := range request.Mappings {
		ids = append(ids, mapping.ActionID)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	op, err := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root).Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || len(op.Actions) != 6 {
		for _, w := range payload.Actions[0].PlannedWrites {
			_, statErr := os.Lstat(filepath.Join(f.root, filepath.FromSlash(w.Path)))
			t.Logf("write %s %s: %v", w.Kind, w.Path, statErr)
		}
		t.Fatalf("op status=%s error=%s first_action=%s backup=%s err=%v", op.Status, op.Error, op.Actions[0].Error, op.ApplyBackupID, err)
	}
	result, err := NewMigrationVerifyService(plans, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || result.Status != "verified" || len(result.Targets) != 6 {
		t.Fatalf("verify=%+v err=%v", result, err)
	}
	adapter := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(f.root, "rooms"))
	for _, preset := range bedroomLightPresets {
		command, err := adapter.LoadCommand("bedroom", "roomlight", preset.id)
		if err != nil || command.ID != preset.id || command.Name != preset.name || command.Format != models.OperationFormatPresetCommand || command.PicoCommand != preset.pico || command.CarrierKHz != 38 || command.Repeat != 0 {
			t.Fatalf("command=%+v err=%v", command, err)
		}
	}
}

func TestBedroomLightPresetFixtureRejectsAmbiguousMapping(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*models.MigrationDryRunRequest)
	}{
		{"missing_pico_decision", func(r *models.MigrationDryRunRequest) { r.Mappings[0].Decision.PicoCommand = "" }},
		{"status_not_a_send_command", func(r *models.MigrationDryRunRequest) { r.Mappings[0].Decision.PicoCommand = "LIGHT_STATUS_CH3" }},
		{"target_identity_mismatch", func(r *models.MigrationDryRunRequest) { r.Mappings[0].Decision.TargetID = "other" }},
		{"duplicate_target", func(r *models.MigrationDryRunRequest) { r.Mappings[1].TargetID = r.Mappings[0].TargetID }},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			request := bedroomLightFixture(t, f.root)
			change.mutate(&request)
			plan, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(NewMaintenancePlanService(f.state.Plans(), f.backup))).Create(request)
			if change.name == "missing_pico_decision" {
				if err != nil || plan.ReadyToApply {
					t.Fatalf("incomplete decision plan=%+v err=%v", plan, err)
				}
			} else if err == nil || (!errors.Is(err, ErrMigrationPlanInvalid) && !errors.Is(err, ErrMigrationDryRunInvalid)) {
				t.Fatalf("ambiguous mapping: ready=%v err=%v", plan.ReadyToApply, err)
			}
		})
	}
}

func TestBedroomLightPresetAndPicoThreeFrameContract(t *testing.T) {
	if len(bedroomPresetSpecs) != len(bedroomLightPresets) {
		t.Fatalf("preset count = %d, want %d", len(bedroomPresetSpecs), len(bedroomLightPresets))
	}
	firmware, err := os.ReadFile("../../pico-firmware/bedroom-usb/command_handler.cpp")
	if err != nil {
		t.Fatal(err)
	}
	text := string(firmware)
	if !strings.Contains(text, "for (int i = 0; i < 3; i++) {\n        ir_send_panasonic_40bit(code);") {
		t.Fatal("Pico preset is no longer three frames")
	}
	for _, preset := range bedroomLightPresets {
		spec, ok := bedroomPresetSpecFor(preset.id)
		if !ok || spec.Name != preset.name || spec.Pico != preset.pico || !iradapter.IsAllowedPresetCommand(preset.pico) {
			t.Fatalf("Roomlight Command mapping mismatch: %s", preset.pico)
		}
		if !strings.Contains(text, `strcmp(command, "`+preset.pico+`") == 0`) || !strings.Contains(text, `send_light_command("`+preset.action+`",`) {
			t.Fatalf("firmware mapping mismatch: %s", preset.pico)
		}
	}
	if iradapter.IsAllowedPresetCommand("LIGHT_STATUS_CH3") {
		t.Fatal("status became an IR send command")
	}
}
