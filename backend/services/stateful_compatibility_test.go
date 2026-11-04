package services

import (
	"backend/models"
	"backend/repositories"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const legacyStatefulSchema = `{"power":{"type":"enum","values":["on","off"]},"note":{"type":"string","min":0},"level":{"type":"number","values":["unused"]},"choice":{"type":"enum","values":[""]}}`
const legacyStatefulState = `{"id":"saved","name":"Saved","format":"raw_samples","carrier_khz":38,"repeat":0,"state":{"power":"on","note":"abc","level":0,"choice":""},"count":4,"timings_us":[100,200,300,400],"captures":[{"ok":true,"valid":true,"count":4,"timings_us":[100,200,300,400]},{"ok":true,"valid":false,"count":2,"timings_us":[50,60],"error":"too short"}]}`

func statefulFixture(t *testing.T, f cleanupApplyFixture, id, schema, state string) string {
	t.Helper()
	base := "rooms/r/devices/" + id
	f.put(t, "rooms/r/room.json", `{"id":"r","name":"Room"}`)
	f.put(t, base+"/device.json", `{"id":"`+id+`","room_id":"r","name":"Device","device_type":"air_conditioner","primary_adapter":"ir"}`)
	f.put(t, base+"/adapters/ir/ir_config.json", `{"ir_remote_type":"stateful","carrier_khz":38,"default_repeat":1}`)
	f.put(t, base+"/adapters/ir/state_schema.json", schema)
	f.put(t, base+"/adapters/ir/states/saved.json", state)
	return base
}
func treeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		out[rel] = string(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func requireTree(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(before, treeBytes(t, root)) {
		t.Fatal("fixture bytes changed: " + root)
	}
}
func contractSchemas(t *testing.T) map[string]struct {
	Schema models.IRStateSchema `json:"schema"`
	States []struct {
		Value map[string]interface{} `json:"value"`
		Valid bool                   `json:"valid"`
	} `json:"states"`
} {
	t.Helper()
	data, err := os.ReadFile("testdata/ir_state_schema_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Schema models.IRStateSchema `json:"schema"`
		States []struct {
			Value map[string]interface{} `json:"value"`
			Valid bool                   `json:"valid"`
		} `json:"states"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func TestStatefulCompatibilityReadbackHealthBackupAndConflict(t *testing.T) {
	fixtures := contractSchemas(t)
	cases := map[string][2]string{"legacy": {legacyStatefulSchema, legacyStatefulState}}
	for name, fixture := range fixtures {
		schema, _ := json.Marshal(fixture.Schema)
		state := models.IRState{ID: "saved", Name: "Saved", Format: models.OperationFormatRawSamples, CarrierKHz: 38, Count: 4, TimingsUS: []uint{100, 200, 300, 400}, Captures: []models.IRCapture{{OK: true, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}}}
		for _, tc := range fixture.States {
			if tc.Valid {
				state.State = tc.Value
				break
			}
		}
		encoded, _ := json.Marshal(state)
		cases[name] = [2]string{string(schema), string(encoded)}
	}
	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			base := statefulFixture(t, f, "old", pair[0], pair[1])
			statefulFixture(t, f, "archived", pair[0], pair[1])
			if _, err := f.trash.Move(repositories.TrashMoveRequest{EntryID: "entry_archived", SourceRelativePath: "rooms/r/devices/archived", OriginalID: "archived", TargetType: "device", Reason: "fixture archive", OperationType: "delete", OperationID: "op_archive"}); err != nil {
				t.Fatal(err)
			}
			trashBefore := treeBytes(t, f.trashRoot)
			before := treeBytes(t, f.root)
			var expected models.IRState
			if err := json.Unmarshal([]byte(pair[1]), &expected); err != nil {
				t.Fatal(err)
			}
			schema := schemaFromJSON(t, pair[0])
			for i := 0; i < 2; i++ { // Rebuild all runtime dependencies, not a process restart.
				service := NewIRAdapterServiceWithRoomsDir(filepath.Join(f.root, "rooms"))
				got, err := service.GetState("r", "old", "saved")
				if err != nil || !reflect.DeepEqual(got, expected) {
					t.Fatalf("state readback=%+v err=%v", got, err)
				}
				read, err := service.GetStateSchema("r", "old")
				if err != nil || !reflect.DeepEqual(read, schema) {
					t.Fatalf("schema readback=%+v err=%v", read, err)
				}
				if err := validateIRStateMatchesSchema(got.State, read); err != nil {
					t.Fatal(err)
				}
				issues, err := NewDataHealthService(repositories.NewDataHealthRepository(f.root)).Scan()
				if err != nil || len(issues) != 0 {
					t.Fatalf("health=%+v err=%v", issues, err)
				}
			}
			requireTree(t, f.root, before)
			backup := NewBackupService(f.backup, f.state.Operations())
			detail, err := f.backup.Create(repositories.BackupCreate{ID: "backup_contract", Role: "rehearsal", PlanID: "plan_contract", ActionIDs: []string{"snapshot"}, Sources: []string{"rooms/r"}})
			if err != nil {
				t.Fatal(err)
			}
			bundleRoot := filepath.Join(filepath.Dir(f.root), "backup", detail.Bundle.ID)
			snapshot := treeBytes(t, filepath.Join(bundleRoot, "data"))
			inventory, err := os.ReadFile(filepath.Join(bundleRoot, "inventory.json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.RestoreRehearsal(detail.Bundle.ID); err != nil {
				t.Fatal(err)
			}
			isolated := filepath.Join(filepath.Dir(f.root), "backup", ".restore-rehearsal", detail.Bundle.ID)
			read, err := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(isolated, "rooms")).LoadState("r", "old", "saved")
			if err != nil || !reflect.DeepEqual(read, expected) {
				t.Fatalf("isolated=%+v err=%v", read, err)
			}
			requireTree(t, filepath.Join(bundleRoot, "data"), snapshot)
			requireTree(t, f.root, before)
			if _, err := f.trash.Load("entry_archived"); err != nil {
				t.Fatal(err)
			}
			requireTree(t, f.trashRoot, trashBefore)
			readInventory, err := os.ReadFile(filepath.Join(bundleRoot, "inventory.json"))
			if err != nil || string(readInventory) != string(inventory) {
				t.Fatal("sealed Backup inventory changed")
			}
			changed := schemaFromJSON(t, `{"unrelated":{"type":"boolean"}}`)
			_, err = NewIRAdapterServiceWithRoomsDir(filepath.Join(f.root, "rooms")).SaveStateSchema("r", "old", changed)
			assertServiceErrorKind(t, err, ErrorConflict)
			requireTree(t, f.root, before)
			if _, err := os.Stat(filepath.Join(f.root, base, "adapters/ir/states/saved.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStatefulCompatibilityDiagnosticsAndBackupRejection(t *testing.T) {
	for _, tc := range []struct{ name, schema, state, kind string }{
		{"unknown_attribute", `{"power":{"type":"enum","values":["on"],"future":true}}`, legacyStatefulState, "invalid_state_schema"},
		{"invalid_presence", `{"power":{"type":"enum","values":["on"],"presence":"future"}}`, legacyStatefulState, "invalid_state_schema"},
		{"state_mismatch", legacyStatefulSchema, strings.Replace(legacyStatefulState, `"power":"on"`, `"power":"invalid"`, 1), "state_schema_mismatch"},
		{"unknown_signal_format", legacyStatefulSchema, strings.Replace(legacyStatefulState, `"format":"raw_samples"`, `"format":"future"`, 1), "unsupported_format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			statefulFixture(t, f, "old", tc.schema, tc.state)
			before := treeBytes(t, f.root)
			issues, err := NewDataHealthService(repositories.NewDataHealthRepository(f.root)).Scan()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, issue := range issues {
				if strings.HasPrefix(issue.ID, tc.kind+":") {
					found = true
					t.Logf("diagnostic=%s message=%s", issue.ID, issue.Message)
				}
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.kind, issues)
			}
			backup := NewBackupService(f.backup, nil)
			detail, err := f.backup.Create(repositories.BackupCreate{ID: "backup_invalid", Role: "rehearsal", PlanID: "plan_contract", ActionIDs: []string{"snapshot"}, Sources: []string{"rooms/r"}})
			if err != nil {
				t.Fatal(err)
			}
			snapshotRoot := filepath.Join(filepath.Dir(f.root), "backup", detail.Bundle.ID, "data")
			snap := treeBytes(t, snapshotRoot)
			_, restoreErr := backup.RestoreRehearsal(detail.Bundle.ID)
			var stageErr *repositories.BackupRestoreError
			if !errors.As(restoreErr, &stageErr) || stageErr.Stage != repositories.BackupRestoreStageRepositoryReadback {
				t.Fatalf("restore rejection=%v", restoreErr)
			}
			after, err := f.backup.Load(detail.Bundle.ID)
			if err != nil || after.Registry.State != "snapshot_verified" {
				t.Fatalf("incompatible backup promoted: %+v %v", after.Registry, err)
			}
			requireTree(t, f.root, before)
			requireTree(t, snapshotRoot, snap)
		})
	}
}

func TestStatefulCompatibilityMigrationPlanReadback(t *testing.T) {
	for _, schema := range []models.IRStateSchema{schemaFromJSON(t, legacyStatefulSchema), contractSchemas(t)["air_conditioner"].Schema, contractSchemas(t)["non_air_conditioner"].Schema} {
		f := newCleanupApplyFixture(t)
		writeMigrationFixture(t, f.root, "raw_samples")
		f.put(t, "rooms/r/devices/d/adapters/ir/ir_config.json", `{"ir_remote_type":"stateful","carrier_khz":38}`)
		request := migrationDryRunRequest("raw_samples")
		m := &request.Mappings[0]
		m.TargetKind = "state"
		m.IRConfig = nil
		encodedSchema, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		f.put(t, "rooms/r/devices/d/adapters/ir/state_schema.json", string(encodedSchema))
		m.StateSchema = nil
		switch {
		case schema["note"].Min != nil:
			m.Decision.State = map[string]interface{}{"power": "on", "note": "abc", "level": float64(0), "choice": ""}
		case schema["mode"].Type != "":
			m.Decision.State = map[string]interface{}{"power": "on", "mode": "fan"}
		default:
			m.Decision.State = map[string]interface{}{"enabled": false, "profile": "normal"}
		}
		common := NewMaintenancePlanService(f.state.Plans(), f.backup)
		plans := NewMigrationPlanService(common)
		plan, err := NewMigrationDryRunService(f.root, plans).Create(request)
		if err != nil || !plan.ReadyToApply {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		read, err := NewMigrationPlanService(NewMaintenancePlanService(f.state.Plans(), f.backup)).Load(plan.PlanID)
		var savedPayload, originalPayload interface{}
		if decodeErr := json.Unmarshal(read.Payload, &savedPayload); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if decodeErr := json.Unmarshal(plan.Payload, &originalPayload); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if err != nil || read.Digest != plan.Digest || !reflect.DeepEqual(savedPayload, originalPayload) {
			t.Fatalf("plan readback: %v", err)
		}
		backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
		rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
			t.Fatal(err)
		}
		op, err := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root).Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
		if err != nil || op.Status != "verified" {
			t.Fatalf("apply=%+v err=%v", op, err)
		}
		verified, err := NewMigrationVerifyService(plans, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: op.ID})
		if err != nil || verified.Status != "verified" {
			t.Fatalf("verify=%+v err=%v", verified, err)
		}
	}
}

func TestStatefulCompatibilitySeparateDeviceSuccessAndFailurePreserveOld(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failed_learning"}[fail], func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			base := statefulFixture(t, f, "old", legacyStatefulSchema, legacyStatefulState)
			oldRoot := filepath.Join(f.root, base)
			before := treeBytes(t, oldRoot)
			backup := NewBackupService(f.backup, nil)
			detail, err := f.backup.Create(repositories.BackupCreate{ID: "backup_old", Role: "rehearsal", PlanID: "plan_old", ActionIDs: []string{"snapshot"}, Sources: []string{"rooms/r"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.RestoreRehearsal(detail.Bundle.ID); err != nil {
				t.Fatal(err)
			}
			devices := repositories.NewDeviceRepositoryWithRoomsDir(filepath.Join(f.root, "rooms"))
			if err := devices.Save(models.Device{ID: "new", Name: "New", RoomID: "r", DeviceType: "air_conditioner", PrimaryAdapter: "ir"}); err != nil {
				t.Fatal(err)
			}
			service := NewIRAdapterServiceWithRoomsDir(filepath.Join(f.root, "rooms"))
			if _, err := service.SaveConfig("r", "new", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
				t.Fatal(err)
			}
			schema := contractSchemas(t)["air_conditioner"].Schema
			if _, err := service.SaveStateSchema("r", "new", schema); err != nil {
				t.Fatal(err)
			}
			values := map[string]interface{}{"power": "on", "mode": "fan"}
			format := models.OperationFormatRawSamples
			if _, err := service.CreateState("r", "new", models.IRStateWriteRequest{ID: stringPointer("fan"), Name: stringPointer("Fan"), State: &values, Format: &format}); err != nil {
				t.Fatal(err)
			}
			service.capture = func(int) models.IRCapture {
				if fail {
					return models.IRCapture{Error: "synthetic failure"}
				}
				return models.IRCapture{OK: true, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}
			}
			_, err = service.CaptureState("r", "new", "fan", models.IRStateCaptureRequest{Samples: intPointerForStateTest(1), MaxAttempts: intPointerForStateTest(1)})
			if fail {
				if err == nil {
					t.Fatal("failure accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			rebuilt := NewIRAdapterServiceWithRoomsDir(filepath.Join(f.root, "rooms"))
			selected := "new"
			if fail {
				selected = "old"
			}
			if _, err := devices.Load("r", selected); err != nil {
				t.Fatal(err)
			}
			id := "fan"
			if fail {
				id = "saved"
			}
			state, err := rebuilt.GetState("r", selected, id)
			if err != nil || state.Count != 4 {
				t.Fatalf("selected=%s state=%+v err=%v", selected, state, err)
			}
			list, err := devices.List("r")
			if err != nil || len(list) != 2 {
				t.Fatalf("device selection=%+v err=%v", list, err)
			}
			requireTree(t, oldRoot, before)
			if fail {
				newState, err := rebuilt.GetState("r", "new", "fan")
				if err != nil || newState.Count != 0 {
					t.Fatal("failed new fixture was deleted or unexpectedly learned")
				}
			}
		})
	}
}

func TestStatefulCompatibilityMigrationRejectsInvalidSchemaAndState(t *testing.T) {
	for _, invalidSchema := range []bool{false, true} {
		t.Run(map[bool]string{false: "state", true: "schema"}[invalidSchema], func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			writeMigrationFixture(t, f.root, "raw_samples")
			f.put(t, "rooms/r/devices/d/adapters/ir/ir_config.json", `{"ir_remote_type":"stateful","carrier_khz":38}`)
			schema := legacyStatefulSchema
			if invalidSchema {
				schema = `{"power":{"type":"enum","values":["on"],"presence":"future"}}`
			}
			f.put(t, "rooms/r/devices/d/adapters/ir/state_schema.json", schema)
			before := treeBytes(t, f.root)
			request := migrationDryRunRequest("raw_samples")
			request.Mappings[0].TargetKind = "state"
			request.Mappings[0].Decision.State = map[string]interface{}{"power": "invalid"}
			plans := NewMigrationPlanService(NewMaintenancePlanService(f.state.Plans(), f.backup))
			if _, err := NewMigrationDryRunService(f.root, plans).Create(request); err == nil {
				t.Fatal("invalid migration accepted")
			}
			requireTree(t, f.root, before)
		})
	}
}
