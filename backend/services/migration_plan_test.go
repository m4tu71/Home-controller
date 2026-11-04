package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

func migrationHash(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func migrationAction() models.MigrationPlanAction {
	target := "rooms/room_1/devices/device_1/adapters/ir/states/cool.json"
	return models.MigrationPlanAction{
		ActionID: "migrate_cool", Action: "write_ir_state",
		Source:       models.MigrationPathSnapshot{Path: "a.bin", Exists: true, Kind: "file", Size: int64(len("source-a")), SHA256: migrationHash("source-a")},
		SourceFormat: "raw_samples", TargetBefore: models.MigrationPathSnapshot{Path: target},
		TargetParentBefore: models.MigrationPathSnapshot{Path: filepath.ToSlash(filepath.Dir(target)), Exists: true, Kind: "directory", SHA256: migrationHash("parent")},
		PlannedWrites:      []models.MigrationPlannedWrite{{Path: target, Kind: "json", Role: "operation", Before: models.MigrationPathSnapshot{Path: target}, ExpectedAfterState: "created"}},
		TargetFormat:       models.OperationFormatRawSamples, Transform: "preserve_raw_samples", ExpectedAfterState: "target_created_source_preserved",
		Decision: &models.MigrationDecision{RoomID: "room_1", DeviceID: "device_1", TargetID: "cool", State: map[string]any{"mode": "cool"}},
	}
}

func TestMigrationPlanCreateLoadAndSharedBackupSelection(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	service := NewMigrationPlanService(f.service)
	before, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Create(models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{migrationAction()}})
	if err != nil || !plan.ReadyToApply || plan.OperationType != "migration" || plan.PayloadSchemaID != "migration.v1" || plan.BackupRequirement != "rehearsal_and_apply" || len(plan.Actions) != 1 || !plan.Actions[0].Selectable || plan.Actions[0].Destructive || plan.PlanID == "" || plan.Digest == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if strings.Contains(string(plan.Payload), "timings_us") || strings.Contains(string(plan.Payload), "captures") {
		t.Fatalf("RAW body copied: %s", plan.Payload)
	}
	loaded, err := service.Load(plan.PlanID)
	if err != nil || loaded.Digest != plan.Digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	selected, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"migrate_cool"})
	if err != nil || len(selected.Sources) != 1 || selected.Sources[0] != "a.bin" {
		t.Fatalf("backup=%+v err=%v", selected, err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, "a.bin"))
	if err != nil || string(data) != "source-a" {
		t.Fatalf("source changed: %q %v", data, err)
	}
	after, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("runtime inventory changed: %v", err)
	}
}

func TestMigrationPlanRejectsStaleSourceSnapshot(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	action := migrationAction()
	action.Source.SHA256 = strings.Repeat("0", 64)
	if _, err := NewMigrationPlanService(f.service).Create(models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{action}}); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("stale source err=%v", err)
	}
	items, err := os.ReadDir(filepath.Join(f.stateDir, "plans"))
	if err != nil || len(items) != 0 {
		t.Fatalf("stale plan persisted: %d %v", len(items), err)
	}
}

func TestMigrationPlanAllowsPlannedParentAndScaffoldWrites(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	action := migrationAction()
	parent := action.TargetParentBefore.Path
	action.TargetParentBefore = models.MigrationPathSnapshot{Path: parent}
	action.PlannedWrites = append(action.PlannedWrites,
		models.MigrationPlannedWrite{Path: parent, Kind: "directory", Role: "directory", Before: models.MigrationPathSnapshot{Path: parent}, ExpectedAfterState: "created"},
		models.MigrationPlannedWrite{Path: "rooms/room_1/room.json", Kind: "json", Role: "room", Before: models.MigrationPathSnapshot{Path: "rooms/room_1/room.json"}, ExpectedAfterState: "created", Content: json.RawMessage(`{"id":"room_1","name":"Room"}`)},
		models.MigrationPlannedWrite{Path: "rooms/room_1/devices/device_1/device.json", Kind: "json", Role: "device", Before: models.MigrationPathSnapshot{Path: "rooms/room_1/devices/device_1/device.json"}, ExpectedAfterState: "created", Content: json.RawMessage(`{"id":"device_1","room_id":"room_1","name":"Device","device_type":"ir","primary_adapter":"ir"}`)},
	)
	plan, err := NewMigrationPlanService(f.service).Create(models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{action}})
	if err != nil || !plan.ReadyToApply {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, parent)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planned parent was created: %v", err)
	}
	other := action
	other.ActionID = "migrate_heat"
	other.TargetBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/states/heat.json"
	other.Decision = &models.MigrationDecision{RoomID: "room_1", DeviceID: "device_1", TargetID: "heat", State: map[string]any{"mode": "heat"}}
	other.PlannedWrites = append([]models.MigrationPlannedWrite(nil), action.PlannedWrites...)
	other.PlannedWrites[0].Path = other.TargetBefore.Path
	other.PlannedWrites[0].Before.Path = other.TargetBefore.Path
	twoActions := []models.MigrationPlanAction{action, other}
	commonActions := []models.MaintenancePlanAction{{ID: action.ActionID, Action: action.Action, Selectable: true, BackupSources: []string{action.Source.Path}}, {ID: other.ActionID, Action: other.Action, Selectable: true, BackupSources: []string{other.Source.Path}}}
	twoPayload, _ := json.Marshal(models.MigrationPlanPayload{Actions: twoActions})
	twoPlan := models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: "migration.v1", BackupRequirement: "rehearsal_and_apply", ReadyToApply: true, Payload: twoPayload, Actions: commonActions}
	if err := ValidateMigrationPlan(twoPlan); err != nil {
		t.Fatalf("shared identical scaffold rejected: %v", err)
	}
	other.PlannedWrites[2].Content = json.RawMessage(`{"id":"room_1","name":"Different"}`)
	twoPlan.Payload = mustMigrationPayloadActions(t, action, other)
	if err := ValidateMigrationPlan(twoPlan); !errors.Is(err, ErrMigrationPlanInvalid) {
		t.Fatalf("conflicting shared scaffold accepted: %v", err)
	}
	invalidScaffold := action
	invalidScaffold.PlannedWrites = append([]models.MigrationPlannedWrite(nil), action.PlannedWrites...)
	invalidScaffold.PlannedWrites[2].Content = json.RawMessage(`{"id":"room_1","name":"Room","timings_us":[1,2]}`)
	if err := ValidateMigrationPlan(models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: "migration.v1", BackupRequirement: "rehearsal_and_apply", ReadyToApply: true, Payload: mustMigrationPayload(t, invalidScaffold), Actions: []models.MaintenancePlanAction{{ID: action.ActionID, Action: action.Action, Selectable: true, BackupSources: []string{action.Source.Path}}}}); !errors.Is(err, ErrMigrationPlanInvalid) {
		t.Fatalf("RAW in scaffold accepted: %v", err)
	}
	action.PlannedWrites = action.PlannedWrites[:1]
	if err := ValidateMigrationPlan(models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: "migration.v1", BackupRequirement: "rehearsal_and_apply", ReadyToApply: true, Payload: mustMigrationPayload(t, action), Actions: []models.MaintenancePlanAction{{ID: action.ActionID, Action: action.Action, Selectable: true, BackupSources: []string{action.Source.Path}}}}); !errors.Is(err, ErrMigrationPlanInvalid) {
		t.Fatalf("missing parent write err=%v", err)
	}
}

func mustMigrationPayload(t *testing.T, action models.MigrationPlanAction) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{action}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustMigrationPayloadActions(t *testing.T, actions ...models.MigrationPlanAction) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(models.MigrationPlanPayload{Actions: actions})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMigrationPlanFormatMappingAndHumanDecision(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*models.MigrationPlanAction)
		ready   bool
		invalid bool
	}{
		{"raw samples complete", func(a *models.MigrationPlanAction) {}, true, false},
		{"raw command", func(a *models.MigrationPlanAction) {
			a.Action = "write_ir_command"
			a.TargetBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands/power.json"
			a.TargetParentBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands"
			a.SourceFormat = "raw"
			a.TargetFormat = models.OperationFormatRaw
			a.Transform = "preserve_raw"
			a.Decision.TargetID = "power"
			a.Decision.State = nil
			a.PlannedWrites[0].Path = a.TargetBefore.Path
			a.PlannedWrites[0].Before.Path = a.TargetBefore.Path
		}, true, false},
		{"raw median requires approval", func(a *models.MigrationPlanAction) {
			a.SourceFormat = "raw_median"
			a.TargetFormat = models.OperationFormatRaw
			a.Transform = "raw_median_to_raw"
		}, false, false},
		{"raw median approved", func(a *models.MigrationPlanAction) {
			a.SourceFormat = "raw_median"
			a.TargetFormat = models.OperationFormatRaw
			a.Transform = "raw_median_to_raw"
			a.Decision.ApproveRawMedian = true
		}, true, false},
		{"preset requires command", func(a *models.MigrationPlanAction) {
			a.Action = "write_ir_command"
			a.TargetBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands/power.json"
			a.TargetParentBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands"
			a.SourceFormat = "preset_command"
			a.TargetFormat = models.OperationFormatPresetCommand
			a.Transform = "preserve_preset_command"
			a.Decision.TargetID = "power"
			a.Decision.State = nil
			a.PlannedWrites[0].Path = a.TargetBefore.Path
			a.PlannedWrites[0].Before.Path = a.TargetBefore.Path
		}, false, false},
		{"preset decided", func(a *models.MigrationPlanAction) {
			a.Action = "write_ir_command"
			a.TargetBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands/power.json"
			a.TargetParentBefore.Path = "rooms/room_1/devices/device_1/adapters/ir/commands"
			a.SourceFormat = "preset_command"
			a.TargetFormat = models.OperationFormatPresetCommand
			a.Transform = "preserve_preset_command"
			a.Decision.TargetID = "power"
			a.Decision.State = nil
			a.Decision.PicoCommand = "LIGHT_ON_CH3"
			a.PlannedWrites[0].Path = a.TargetBefore.Path
			a.PlannedWrites[0].Before.Path = a.TargetBefore.Path
		}, true, false},
		{"state without payload", func(a *models.MigrationPlanAction) { a.Decision.State = nil }, false, false},
		{"no decision", func(a *models.MigrationPlanAction) { a.Decision = nil }, false, false},
		{"invalid mapping", func(a *models.MigrationPlanAction) { a.TargetFormat = models.OperationFormatCode }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			a := migrationAction()
			tc.change(&a)
			plan, err := NewMigrationPlanService(f.service).Create(models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{a}})
			if tc.invalid {
				if !errors.Is(err, ErrMigrationPlanInvalid) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || plan.ReadyToApply != tc.ready || plan.Actions[0].Selectable != tc.ready {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
		})
	}
}

func TestMigrationPlanRejectsSchemaAndConsistencyErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.MigrationPlanPayload)
	}{
		{"missing action", func(p *models.MigrationPlanPayload) { p.Actions = nil }},
		{"missing source hash", func(p *models.MigrationPlanPayload) { p.Actions[0].Source.SHA256 = "" }},
		{"source path traversal", func(p *models.MigrationPlanPayload) { p.Actions[0].Source.Path = "../a.bin" }},
		{"target path traversal", func(p *models.MigrationPlanPayload) { p.Actions[0].TargetBefore.Path = "../cool.json" }},
		{"target already exists", func(p *models.MigrationPlanPayload) { p.Actions[0].TargetBefore.Exists = true }},
		{"target parent mismatch", func(p *models.MigrationPlanPayload) { p.Actions[0].TargetParentBefore.Path = "rooms/other" }},
		{"target parent hash missing", func(p *models.MigrationPlanPayload) { p.Actions[0].TargetParentBefore.SHA256 = "" }},
		{"wrong action", func(p *models.MigrationPlanPayload) { p.Actions[0].Action = "write_ir_command" }},
		{"wrong decision", func(p *models.MigrationPlanPayload) { p.Actions[0].Decision.RoomID = "different" }},
		{"unsupported format", func(p *models.MigrationPlanPayload) { p.Actions[0].SourceFormat = "unknown" }},
		{"bad expected state", func(p *models.MigrationPlanPayload) { p.Actions[0].ExpectedAfterState = "overwritten" }},
		{"missing planned write", func(p *models.MigrationPlanPayload) { p.Actions[0].PlannedWrites = nil }},
		{"duplicate planned write", func(p *models.MigrationPlanPayload) {
			p.Actions[0].PlannedWrites = append(p.Actions[0].PlannedWrites, p.Actions[0].PlannedWrites[0])
		}},
		{"planned write target mismatch", func(p *models.MigrationPlanPayload) { p.Actions[0].PlannedWrites[0].Path = "rooms/other.json" }},
		{"duplicate action", func(p *models.MigrationPlanPayload) { p.Actions = append(p.Actions, p.Actions[0]) }},
		{"duplicate target", func(p *models.MigrationPlanPayload) {
			other := p.Actions[0]
			other.ActionID = "migrate_other"
			p.Actions = append(p.Actions, other)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{migrationAction()}}
			tc.change(&payload)
			if _, err := NewMigrationPlanService(newMaintenancePlanFixture(t).service).Create(payload); !errors.Is(err, ErrMigrationPlanInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	base := models.MigrationPlanPayload{Actions: []models.MigrationPlanAction{migrationAction()}}
	encoded, _ := json.Marshal(base)
	plan := models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: "migration.v1", BackupRequirement: "rehearsal_and_apply", ReadyToApply: true, Payload: encoded, Actions: []models.MaintenancePlanAction{{ID: "migrate_cool", Action: "write_ir_state", Selectable: true, BackupSources: []string{"a.bin"}}}}
	for _, tc := range []struct {
		name   string
		change func(*models.MaintenancePlanEnvelope)
	}{
		{"unknown payload field", func(p *models.MaintenancePlanEnvelope) {
			p.Payload = []byte(strings.TrimSuffix(string(p.Payload), "}") + `,"unexpected":true}`)
		}},
		{"unknown nested field", func(p *models.MaintenancePlanEnvelope) {
			p.Payload = []byte(strings.Replace(string(p.Payload), `"source_format":"raw_samples"`, `"source_format":"raw_samples","unexpected":true`, 1))
		}},
		{"missing required field", func(p *models.MaintenancePlanEnvelope) {
			p.Payload = []byte(strings.Replace(string(p.Payload), `"source_format":"raw_samples",`, "", 1))
		}},
		{"envelope action mismatch", func(p *models.MaintenancePlanEnvelope) { p.Actions[0].BackupSources = []string{"b.bin"} }},
		{"wrong ready flag", func(p *models.MaintenancePlanEnvelope) { p.ReadyToApply = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyPlan := plan
			copyPlan.Actions = append([]models.MaintenancePlanAction(nil), plan.Actions...)
			tc.change(&copyPlan)
			if err := ValidateMigrationPlan(copyPlan); !errors.Is(err, ErrMigrationPlanInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
