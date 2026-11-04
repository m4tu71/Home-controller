package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestMigrationDryRunSavedPlanAndUnchangedRuntime(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	before, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	request := migrationDryRunRequest("raw")
	plan, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(request)
	if err != nil || !plan.ReadyToApply || plan.PlanID == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	loaded, err := NewMigrationPlanService(f.service).Load(plan.PlanID)
	if err != nil || loaded.Digest != plan.Digest {
		t.Fatalf("reload=%+v err=%v", loaded, err)
	}
	after, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("runtime changed: %v", err)
	}
	var payload models.MigrationPlanPayload
	if json.Unmarshal(plan.Payload, &payload) != nil || len(payload.Actions) != 1 || payload.Actions[0].Source.SHA256 == "" || payload.Actions[0].TargetBefore.Exists {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestMigrationDryRunRejectsUnsafeAndCollision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string, *models.MigrationDryRunRequest)
		want   error
	}{
		{"traversal", func(t *testing.T, root string, r *models.MigrationDryRunRequest) {
			r.Mappings[0].SourcePath = "../raw.json"
		}, ErrMigrationDryRunInvalid},
		{"target_collision", func(t *testing.T, root string, r *models.MigrationDryRunRequest) {
			p := filepath.Join(root, "rooms/r/devices/d/adapters/ir/commands/power.json")
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		}, ErrMigrationDryRunConflict},
		{"source_symlink", func(t *testing.T, root string, r *models.MigrationDryRunRequest) {
			if err := os.Remove(filepath.Join(root, "ir-signals/raw.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "a.bin"), filepath.Join(root, "ir-signals/raw.json")); err != nil {
				t.Fatal(err)
			}
		}, ErrMigrationDryRunInvalid},
		{"target_symlink", func(t *testing.T, root string, r *models.MigrationDryRunRequest) {
			p := filepath.Join(root, "rooms/r/devices/d/adapters/ir/commands")
			if err := os.Symlink(filepath.Join(root, "ir-signals"), p); err != nil {
				t.Fatal(err)
			}
		}, ErrMigrationDryRunInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			writeMigrationFixture(t, f.root, "raw")
			r := migrationDryRunRequest("raw")
			tc.mutate(t, f.root, &r)
			_, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestMigrationDryRunFormatsAndDecisions(t *testing.T) {
	for _, format := range []string{"raw", "raw_samples", "raw_median", "preset_command"} {
		t.Run(format, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			writeMigrationFixture(t, f.root, format)
			r := migrationDryRunRequest(format)
			if format == "raw_median" {
				r.Mappings[0].Decision.ApproveRawMedian = true
			}
			if format == "preset_command" {
				r.Mappings[0].Decision.PicoCommand = "LIGHT_ON_CH3"
			}
			plan, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r)
			if err != nil || !plan.ReadyToApply {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
		})
	}
}

func TestMigrationRAWValidation(t *testing.T) {
	base := models.MigrationSourceSignal{Name: "raw_samples", Format: "raw_samples", CarrierKHz: 38, Count: 2, TimingsUS: []uint{100, 200}, Captures: []models.IRCapture{{OK: true, Valid: true, Count: 2, TimingsUS: []uint{100, 200}}, {OK: true, Valid: false, Count: 1, TimingsUS: []uint{50}, Error: "too short"}}}
	if !validMigrationSignal(base) {
		t.Fatal("valid captures with a preserved invalid sample must pass")
	}
	for _, tc := range []struct {
		name string
		edit func(*models.MigrationSourceSignal)
	}{
		{"representative_mismatch", func(s *models.MigrationSourceSignal) { s.TimingsUS[0] = 101 }},
		{"top_count_mismatch", func(s *models.MigrationSourceSignal) { s.Count = 1 }},
		{"capture_count_mismatch", func(s *models.MigrationSourceSignal) { s.Captures[1].Count = 2 }},
		{"capture_over_limit", func(s *models.MigrationSourceSignal) { s.Captures[1].TimingsUS[0] = 100001 }},
		{"valid_without_ok", func(s *models.MigrationSourceSignal) { s.Captures[0].OK = false }},
		{"no_valid_capture", func(s *models.MigrationSourceSignal) { s.Captures[0].Valid = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			s.TimingsUS = append([]uint(nil), base.TimingsUS...)
			s.Captures = append([]models.IRCapture(nil), base.Captures...)
			s.Captures[0].TimingsUS = append([]uint(nil), base.Captures[0].TimingsUS...)
			s.Captures[1].TimingsUS = append([]uint(nil), base.Captures[1].TimingsUS...)
			tc.edit(&s)
			if validMigrationSignal(s) {
				t.Fatal("invalid RAW sample accepted")
			}
		})
	}
	for _, format := range []string{"raw", "raw_median"} {
		s := models.MigrationSourceSignal{Name: format, Format: format, CarrierKHz: 38, Count: 2, TimingsUS: []uint{100, 200}}
		if !validMigrationSignal(s) {
			t.Fatalf("valid %s rejected", format)
		}
		s.TimingsUS[0] = 100001
		if validMigrationSignal(s) {
			t.Fatalf("unsafe %s accepted", format)
		}
	}
}

func TestMigrationRAWHumanDecisionGate(t *testing.T) {
	for _, tc := range []struct {
		name, format, kind string
		decide             func(*models.MigrationDryRunRequest)
	}{
		{"median_without_approval", "raw_median", "command", func(*models.MigrationDryRunRequest) {}},
		{"state_without_payload", "raw", "state", func(*models.MigrationDryRunRequest) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			writeMigrationFixture(t, f.root, tc.format)
			r := migrationDryRunRequest(tc.format)
			r.Mappings[0].TargetKind = tc.kind
			if tc.kind == "state" {
				config := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/ir_config.json")
				if err := os.WriteFile(config, []byte(`{"ir_remote_type":"stateful","carrier_khz":38}`), 0600); err != nil {
					t.Fatal(err)
				}
				r.Mappings[0].StateSchema = models.IRStateSchema{"mode": {Type: models.IRStateFieldTypeEnum, Values: []string{"cool"}}}
			}
			tc.decide(&r)
			plan, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r)
			if err != nil || plan.ReadyToApply || plan.Actions[0].Selectable {
				t.Fatalf("unapproved migration must remain unselectable: plan=%+v err=%v", plan, err)
			}
		})
	}
}

func TestMigrationDryRunPlansMissingMetadataAndState(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	for _, rel := range []string{"rooms/r/room.json", "rooms/r/devices/d/device.json", "rooms/r/devices/d/adapters/ir/ir_config.json"} {
		if err := os.Remove(filepath.Join(f.root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	before, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	r := migrationDryRunRequest("raw")
	r.Mappings[0].TargetKind = "state"
	r.Mappings[0].Decision.State = map[string]any{"mode": "cool"}
	r.Mappings[0].Room = &models.Room{ID: "r", Name: "Room"}
	r.Mappings[0].Device = &models.Device{ID: "d", Name: "Device", RoomID: "r", DeviceType: "ir", PrimaryAdapter: "ir"}
	r.Mappings[0].IRConfig = &models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}
	r.Mappings[0].StateSchema = models.IRStateSchema{"mode": {Type: models.IRStateFieldTypeEnum, Values: []string{"cool", "heat"}}}
	plan, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r)
	if err != nil || !plan.ReadyToApply {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	var payload models.MigrationPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	roles := map[string]bool{}
	for _, w := range payload.Actions[0].PlannedWrites {
		roles[w.Role] = true
	}
	for _, role := range []string{"room", "device", "ir_config", "state_schema", "operation", "directory"} {
		if !roles[role] {
			t.Fatalf("missing role %s: %+v", role, roles)
		}
	}
	after, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("runtime changed: %v", err)
	}
	r.Mappings[0].Decision.State = map[string]any{"mode": "invalid"}
	if _, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r); !errors.Is(err, ErrMigrationDryRunInvalid) {
		t.Fatalf("schema mismatch err=%v", err)
	}
}

func TestMigrationDryRunRejectsInvalidExistingMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, path, data string
		state            bool
	}{
		{"room_id", "rooms/r/room.json", `{"id":"wrong","name":"Room"}`, false},
		{"device_json", "rooms/r/devices/d/device.json", `{`, false},
		{"config_kind", "rooms/r/devices/d/adapters/ir/ir_config.json", `{"ir_remote_type":"stateful","carrier_khz":38}`, false},
		{"state_schema", "rooms/r/devices/d/adapters/ir/state_schema.json", `{"mode":{"type":"enum","values":["heat"]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			writeMigrationFixture(t, f.root, "raw")
			r := migrationDryRunRequest("raw")
			if tc.state {
				r.Mappings[0].TargetKind = "state"
				r.Mappings[0].Decision.State = map[string]any{"mode": "cool"}
				p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/ir_config.json")
				if err := os.WriteFile(p, []byte(`{"ir_remote_type":"stateful","carrier_khz":38}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(f.root, tc.path), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewMigrationDryRunService(f.root, NewMigrationPlanService(f.service)).Create(r); !errors.Is(err, ErrMigrationDryRunInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func migrationDryRunRequest(format string) models.MigrationDryRunRequest {
	return models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{{ActionID: "migrate_power", SourcePath: "ir-signals/" + format + ".json", TargetKind: "command", RoomID: "r", DeviceID: "d", TargetID: "power", Decision: &models.MigrationDecision{RoomID: "r", DeviceID: "d", TargetID: "power"}}}}
}

func writeMigrationFixture(t *testing.T, root, format string) {
	t.Helper()
	for _, dir := range []string{"ir-signals", "rooms/r/devices/d/adapters/ir"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for p, data := range map[string]string{"rooms/r/room.json": `{"id":"r","name":"Room"}`, "rooms/r/devices/d/device.json": `{"id":"d","name":"Device","room_id":"r","device_type":"ir","primary_adapter":"ir"}`, "rooms/r/devices/d/adapters/ir/ir_config.json": `{"ir_remote_type":"command","carrier_khz":38}`} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	signal := models.MigrationSourceSignal{Name: format, Format: format, CarrierKHz: 38, Count: 2, TimingsUS: []uint{100, 200}}
	if format == "raw_samples" {
		signal.Captures = []models.IRCapture{{OK: true, Valid: true, Count: 2, TimingsUS: []uint{100, 200}}}
	}
	if format == "preset_command" {
		signal.Count = 0
		signal.TimingsUS = nil
	}
	data, err := json.Marshal(signal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ir-signals", format+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
