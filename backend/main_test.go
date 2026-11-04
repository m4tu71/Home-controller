package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/controller_client"
	"backend/handlers"
	"backend/models"
	"backend/repositories"
	"backend/services"
	appconfig "webcontroller/config"
)

type maintenanceReadbackFixture struct {
	config      appconfig.Config
	planID      string
	planDigest  string
	planAction  string
	operationID string
	backupID    string
	trashID     string
}

func newMaintenanceReadbackFixture(t *testing.T, label string) maintenanceReadbackFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), label)
	dataRoot := filepath.Join(base, "data")
	stateRoot := filepath.Join(base, "maintenance")
	backupRoot := filepath.Join(base, "backups")
	trashRoot := filepath.Join(base, "trash")
	softwareRoot := filepath.Join(base, "software")
	webRoot := filepath.Join(softwareRoot, "web")
	for _, dir := range []string{filepath.Join(dataRoot, "rooms"), filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), backupRoot, trashRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := appconfig.Config{
		HTTPListen: "127.0.0.1:3000", DataRoot: dataRoot, MaintenanceRoot: stateRoot,
		BackupRoot: backupRoot, TrashRoot: trashRoot, TrashRetentionDays: 30,
		SoftwareRoot: softwareRoot, WebRoot: webRoot,
		SocketPath: "/run/web-controller/test.sock", PicoSerialPath: "/dev/serial/by-id/usb-test-if00",
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{
		Dir: stateRoot, RuntimeDataDir: dataRoot, RepositoryDir: softwareRoot,
		WebRootDir: webRoot, BackupDir: backupRoot, TrashDir: trashRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	operationID := label + "_operation"
	if err := state.Operations().Create(models.MaintenanceOperation{
		Version: 1, ID: operationID, Revision: 1, StartedAt: time.Now().UTC(),
		Target: "fixture", Type: "fixture", Actions: []models.MaintenanceOperationAction{}, Status: "verified",
	}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(dataRoot, "ir-signals"),
		filepath.Join(dataRoot, "rooms", "fixture", "devices", "device", "adapters", "ir", "commands"),
	} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		"rooms/fixture/room.json":                                    `{"id":"fixture","name":"Fixture"}`,
		"rooms/fixture/devices/device/device.json":                   `{"id":"device","name":"Device","room_id":"fixture","device_type":"ir","primary_adapter":"ir"}`,
		"rooms/fixture/devices/device/adapters/ir/ir_config.json":    `{"ir_remote_type":"command","carrier_khz":38}`,
		"rooms/fixture/devices/device/adapters/ir/commands/bad.json": `{`,
		"ir-signals/fixture.json":                                    `{"name":"fixture","room":"fixture","device":"device","format":"raw","carrier_khz":38,"count":2,"timings_us":[100,200]}`,
	} {
		if err := os.WriteFile(filepath.Join(dataRoot, filepath.FromSlash(path)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := repositories.NewBackupRepository(repositories.BackupConfig{
		Dir: backupRoot, RuntimeDataDir: dataRoot, RepositoryDir: softwareRoot,
		WebRootDir: webRoot, StateDir: stateRoot, TrashDir: trashRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	commonPlans := services.NewMaintenancePlanService(state.Plans(), backup)
	plan, err := services.NewMigrationDryRunService(dataRoot, services.NewMigrationPlanService(commonPlans)).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{{
		ActionID: "migrate_fixture", SourcePath: "ir-signals/fixture.json", TargetKind: "command",
		RoomID: "fixture", DeviceID: "device", TargetID: "power",
		Decision: &models.MigrationDecision{RoomID: "fixture", DeviceID: "device", TargetID: "power"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	planID := plan.PlanID
	planAction := plan.Actions[0].ID
	backupID := label + "_backup"
	if _, err := backup.Create(repositories.BackupCreate{ID: backupID, Role: "rehearsal", PlanID: planID, ActionIDs: []string{planAction}, Sources: []string{"ir-signals/fixture.json"}}); err != nil {
		t.Fatal(err)
	}
	trashFile := filepath.Join(dataRoot, "rooms", "trash.json")
	if err := os.WriteFile(trashFile, []byte(`{"fixture":"trash"}`), 0600); err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{
		Dir: trashRoot, RuntimeDataDir: dataRoot, RepositoryDir: softwareRoot,
		WebRootDir: webRoot, StateDir: stateRoot, BackupDir: backupRoot, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	trashID := label + "_trash"
	if _, err := trash.Move(repositories.TrashMoveRequest{
		EntryID: trashID, SourceRelativePath: "rooms/trash.json", OriginalID: "trash",
		TargetType: "fixture", Reason: "test fixture", OperationType: "fixture", OperationID: operationID,
	}); err != nil {
		t.Fatal(err)
	}
	return maintenanceReadbackFixture{config: config, planID: planID, planDigest: plan.Digest, planAction: planAction, operationID: operationID, backupID: backupID, trashID: trashID}
}

func assertPlanReadback(t *testing.T, state *repositories.MaintenanceState, fixture maintenanceReadbackFixture) {
	t.Helper()
	payload, err := state.Plans().Load(fixture.planID)
	if err != nil {
		t.Fatalf("plan readback failed: %v", err)
	}
	var envelope models.MaintenancePlanEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("plan payload=%s err=%v", payload, err)
	}
	if envelope.PlanID != fixture.planID || envelope.Digest != fixture.planDigest || envelope.Version != models.MaintenancePlanVersion || envelope.OperationType != "migration" || len(envelope.Actions) != 1 || envelope.Actions[0].ID != fixture.planAction || len(envelope.Actions[0].BackupSources) != 1 || envelope.Actions[0].BackupSources[0] != "ir-signals/fixture.json" {
		t.Fatalf("plan envelope=%+v", envelope)
	}
	var migrationPayload models.MigrationPlanPayload
	if err := json.Unmarshal(envelope.Payload, &migrationPayload); err != nil || len(migrationPayload.Actions) != 1 || migrationPayload.Actions[0].Source.Path != "ir-signals/fixture.json" || migrationPayload.Actions[0].TargetBefore.Path != "rooms/fixture/devices/device/adapters/ir/commands/power.json" || filepath.IsAbs(migrationPayload.Actions[0].Source.Path) || filepath.IsAbs(migrationPayload.Actions[0].TargetBefore.Path) {
		t.Fatalf("migration payload=%+v err=%v", migrationPayload, err)
	}
	loaded, err := services.NewMaintenancePlanService(state.Plans(), nil).Load(fixture.planID)
	if err != nil || loaded.Digest != fixture.planDigest {
		t.Fatalf("typed plan load=%+v err=%v", loaded, err)
	}
}

func assertMaintenanceReadback(t *testing.T, mux *http.ServeMux, fixture maintenanceReadbackFixture) {
	t.Helper()
	checks := []struct {
		method string
		path   string
		body   string
		status int
		match  string
	}{
		{http.MethodGet, "/api/controller/maintenance/operations", "", http.StatusOK, fixture.operationID},
		{http.MethodGet, "/api/controller/maintenance/trash", "", http.StatusOK, fixture.trashID},
		{http.MethodGet, "/api/controller/maintenance/trash/" + fixture.trashID, "", http.StatusOK, fixture.trashID},
		{http.MethodPost, "/api/controller/maintenance/trash/" + fixture.trashID + "/restore/dry-run", "", http.StatusOK, fixture.trashID},
		{http.MethodGet, "/api/controller/maintenance/backups/" + fixture.backupID, "", http.StatusOK, fixture.backupID},
		{http.MethodPost, "/api/controller/maintenance/backups/" + fixture.backupID + "/restore/dry-run", "", http.StatusOK, fixture.backupID},
		{http.MethodGet, "/api/controller/maintenance/data-health", "", http.StatusOK, "\"ok\":true"},
	}
	for _, check := range checks {
		request := httptest.NewRequest(check.method, check.path, strings.NewReader(check.body))
		if check.method == http.MethodPost {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != check.status || !strings.Contains(response.Body.String(), check.match) {
			t.Fatalf("%s %s status=%d body=%s; want status=%d containing %q", check.method, check.path, response.Code, response.Body.String(), check.status, check.match)
		}
	}
}

func TestMaintenanceReadbackUsesConfiguredRootsAndSavedHistory(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "new")
	for _, key := range []string{"WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "WEB_CONTROLLER_BACKUP_DIR", "WEB_CONTROLLER_TRASH_DIR", "WEB_CONTROLLER_TRASH_RETENTION_DAYS"} {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })
	assertMaintenanceReadback(t, newHTTPMuxWithConfig(fixture.config), fixture)
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{
		Dir: fixture.config.MaintenanceRoot, RuntimeDataDir: fixture.config.DataRoot,
		RepositoryDir: fixture.config.SoftwareRoot, WebRootDir: fixture.config.WebRoot,
		BackupDir: fixture.config.BackupRoot, TrashDir: fixture.config.TrashRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPlanReadback(t, state, fixture)
}

func TestLegacyMaintenanceReadbackFixtureRemainsAvailable(t *testing.T) {
	fixture := newLegacySavedHistoryFixture(t)
	t.Setenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR", fixture.config.MaintenanceRoot)
	t.Setenv("WEB_CONTROLLER_BACKUP_DIR", fixture.config.BackupRoot)
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", fixture.config.TrashRoot)
	t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", "30")
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })
	assertMaintenanceReadback(t, newHTTPMuxWithDataRoot(filepath.Join(fixture.config.DataRoot, "rooms")), fixture)
	state, err := repositories.NewMaintenanceStateFromEnvWithRuntimeDataDir(fixture.config.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := state.Plans().Load(fixture.planID)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(payload, &plan); err != nil || plan["plan_id"] != fixture.planID || plan["operation_type"] != "migration" {
		t.Fatalf("legacy plan readback=%s err=%v", payload, err)
	}
	var legacyPlan struct {
		Payload struct {
			Actions []struct {
				ActionID string `json:"action_id"`
				Source   struct {
					Path string `json:"path"`
				} `json:"source"`
				Target struct {
					Path string `json:"path"`
				} `json:"target"`
			} `json:"actions"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &legacyPlan); err != nil || len(legacyPlan.Payload.Actions) != 1 || legacyPlan.Payload.Actions[0].ActionID != "legacy_action" || legacyPlan.Payload.Actions[0].Source.Path != "legacy/fixture.json" || legacyPlan.Payload.Actions[0].Target.Path != "legacy/fixture.out.json" || filepath.IsAbs(legacyPlan.Payload.Actions[0].Source.Path) || filepath.IsAbs(legacyPlan.Payload.Actions[0].Target.Path) {
		t.Fatalf("legacy plan payload=%+v err=%v", legacyPlan, err)
	}
	if _, err := services.NewMaintenancePlanService(state.Plans(), nil).Load(fixture.planID); !errors.Is(err, services.ErrMaintenancePlanInvalid) {
		t.Fatalf("digestless legacy plan must be classified as incompatible: %v", err)
	}
	operation, err := state.Operations().Load(fixture.operationID)
	if err != nil || operation.Status != "verified" || operation.Type != "" || len(operation.ClaimedPaths) != 0 {
		t.Fatalf("legacy operation readback=%+v err=%v", operation, err)
	}
	backups, err := repositories.NewBackupRepository(repositories.BackupConfig{
		Dir: fixture.config.BackupRoot, RuntimeDataDir: fixture.config.DataRoot,
		RepositoryDir: fixture.config.SoftwareRoot, WebRootDir: fixture.config.WebRoot,
		StateDir: fixture.config.MaintenanceRoot, TrashDir: fixture.config.TrashRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := backups.Load(fixture.backupID)
	if err != nil || backup.Bundle.PlanID != fixture.planID {
		t.Fatalf("legacy backup readback=%+v err=%v", backup, err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{
		Dir: fixture.config.TrashRoot, RuntimeDataDir: fixture.config.DataRoot,
		RepositoryDir: fixture.config.SoftwareRoot, WebRootDir: fixture.config.WebRoot,
		StateDir: fixture.config.MaintenanceRoot, BackupDir: fixture.config.BackupRoot, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := trash.Load(fixture.trashID)
	if err != nil || entry.Status != "sealed" || entry.OperationID != fixture.operationID {
		t.Fatalf("legacy Trash readback=%+v err=%v", entry, err)
	}
}

// newLegacySavedHistoryFixture writes records in the v1 saved-file shape
// directly. It deliberately does not call current repository Create/Move
// methods, so readback compatibility is tested against persisted data rather
// than against records produced by the current implementation.
func newLegacySavedHistoryFixture(t *testing.T) maintenanceReadbackFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "legacy_saved")
	dataRoot := filepath.Join(base, "data")
	stateRoot := filepath.Join(base, "maintenance")
	backupRoot := filepath.Join(base, "backups")
	trashRoot := filepath.Join(base, "trash")
	softwareRoot := filepath.Join(base, "software")
	webRoot := filepath.Join(softwareRoot, "web")
	for _, dir := range []string{filepath.Join(dataRoot, "legacy"), filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), backupRoot, trashRoot, softwareRoot, webRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := appconfig.Config{HTTPListen: "127.0.0.1:3000", DataRoot: dataRoot, MaintenanceRoot: stateRoot, BackupRoot: backupRoot, TrashRoot: trashRoot, TrashRetentionDays: 30, SoftwareRoot: softwareRoot, WebRoot: webRoot, SocketPath: "/run/web-controller/test.sock", PicoSerialPath: "/dev/serial/by-id/usb-test-if00"}
	const (
		planID      = "legacy_plan"
		operationID = "legacy_operation"
		backupID    = "legacy_backup"
		trashID     = "legacy_trash"
	)
	created := "2025-01-02T03:04:05Z"
	content := []byte(`{"legacy":true}`)
	dataPath := filepath.Join(dataRoot, "legacy", "fixture.json")
	if err := os.WriteFile(dataPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	writeJSON := func(path string, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(stateRoot, "operations", operationID+".json"), `{"version":1,"id":"legacy_operation","revision":1,"started_at":"2025-01-02T03:04:05Z","target":"legacy/fixture.json","actions":[{"id":"legacy_action","action":"migrate","status":"verified"}],"status":"verified"}`)
	writeJSON(filepath.Join(stateRoot, "plans", planID+".json"), `{"version":1,"plan_id":"legacy_plan","operation_type":"migration","payload_schema_id":"migration.v1","payload":{"actions":[{"action_id":"legacy_action","source":{"path":"legacy/fixture.json"},"target":{"path":"legacy/fixture.out.json"}}]},"created_at":"2025-01-02T03:04:05Z","ready_to_apply":true,"backup_requirement":"rehearsal_and_apply","actions":[{"action_id":"legacy_action","action":"migrate","selectable":true,"destructive":true,"backup_sources":["legacy/fixture.json"]}]}`)
	fileHash := sha256.Sum256(content)
	fileSHA := hex.EncodeToString(fileHash[:])
	files := []models.BackupFile{{Path: "legacy/fixture.json", Size: int64(len(content)), SHA256: fileSHA, JSONStatus: "valid"}}
	dirs := []string{}
	manifestBytes, err := json.Marshal(struct {
		Files       []models.BackupFile `json:"files"`
		Directories []string            `json:"directories"`
	}{files, dirs})
	if err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	backupDir := filepath.Join(backupRoot, backupID)
	if err := os.MkdirAll(filepath.Join(backupDir, "data", "legacy"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "data", "legacy", "fixture.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	writeJSON(filepath.Join(backupDir, "registry.json"), `{"version":1,"id":"legacy_backup","role":"rehearsal","state":"snapshot_verified","updated_at":"2025-01-02T03:04:05Z"}`)
	bundle := map[string]any{"version": 1, "id": backupID, "role": "rehearsal", "plan_id": planID, "action_ids": []string{"legacy_action"}, "created_at": created, "sources": []string{"legacy/fixture.json"}, "files": files, "directories": dirs, "file_count": 1, "total_size": len(content), "inventory_hash": "sha256:" + hex.EncodeToString(manifestHash[:])}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(filepath.Join(backupDir, "inventory.json"), string(bundleJSON))
	trashDir := filepath.Join(trashRoot, trashID)
	if err := os.MkdirAll(filepath.Join(trashDir, "payload"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashDir, "payload", "fixture.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	deleted := "2024-12-01T03:04:05Z"
	eligible := "2024-12-31T03:04:05Z"
	trashHash := hex.EncodeToString(fileHash[:])
	writeJSON(filepath.Join(trashDir, "metadata.json"), `{"version":1,"id":"legacy_trash","status":"sealed","original_relative_path":"legacy/fixture.json","original_id":"fixture","target_type":"legacy","reason":"legacy migration","operation_type":"migration","operation_id":"legacy_operation","before_inventory":{"kind":"file","file_count":1,"total_size":15,"sha256":"`+trashHash+`"},"deleted_at":"`+deleted+`","eligible_after":"`+eligible+`","restore_available":true}`)
	return maintenanceReadbackFixture{config: config, planID: planID, operationID: operationID, backupID: backupID, trashID: trashID}
}

func TestConfiguredMaintenanceRoutesAreWired(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "routes")
	for _, key := range []string{"WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "WEB_CONTROLLER_BACKUP_DIR", "WEB_CONTROLLER_TRASH_DIR", "WEB_CONTROLLER_TRASH_RETENTION_DAYS"} {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })
	mux := newHTTPMuxWithConfig(fixture.config)
	checks := []struct {
		method string
		path   string
		body   string
		empty  bool
	}{
		{http.MethodPost, "/api/controller/maintenance/backups", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/data-cleanup/dry-run", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/migration/dry-run", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/bedroom-preset/dry-run", "", true},
		{http.MethodPost, "/api/controller/maintenance/migration/apply", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/migration/verify", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/migration/rollback/dry-run", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/migration/rollback/apply", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/data-cleanup/apply", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/legacy-ir-cutover/dry-run", `{}`, false},
		{http.MethodPost, "/api/controller/maintenance/legacy-ir-cutover/apply", `{}`, false},
	}
	for _, check := range checks {
		request := httptest.NewRequest(check.method, check.path, strings.NewReader(check.body))
		if !check.empty {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code == http.StatusNotFound || response.Code == http.StatusServiceUnavailable {
			t.Fatalf("%s %s was not wired: status=%d body=%s", check.method, check.path, response.Code, response.Body.String())
		}
	}
}

func postMaintenanceJSON(t *testing.T, mux *http.ServeMux, path string, payload any) (int, map[string]any, string) {
	t.Helper()
	body := ""
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = string(encoded)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	decoded := map[string]any{}
	if response.Body.Len() > 0 {
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("POST %s returned invalid JSON: %s", path, response.Body.String())
		}
	}
	return response.Code, decoded, response.Body.String()
}

func requireMaintenanceStatus(t *testing.T, path string, status int, body string, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("POST %s status=%d body=%s want=%d", path, status, body, want)
	}
}

func TestConfiguredMaintenanceRoutesNormalPaths(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "normal")
	for _, key := range []string{"WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "WEB_CONTROLLER_BACKUP_DIR", "WEB_CONTROLLER_TRASH_DIR", "WEB_CONTROLLER_TRASH_RETENTION_DAYS"} {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })
	mux := newHTTPMuxWithConfig(fixture.config)

	trashDryRunPath := "/api/controller/maintenance/trash/" + fixture.trashID + "/restore/dry-run"
	status, body, raw := postMaintenanceJSON(t, mux, trashDryRunPath, nil)
	requireMaintenanceStatus(t, trashDryRunPath, status, raw, http.StatusOK)
	expectedInventoryHash, ok := body["expected_inventory_hash"].(string)
	if !ok || expectedInventoryHash == "" {
		t.Fatalf("Trash restore preview=%s", raw)
	}
	trashApplyPath := "/api/controller/maintenance/trash/" + fixture.trashID + "/restore"
	status, _, raw = postMaintenanceJSON(t, mux, trashApplyPath, models.TrashRestoreRequest{ExpectedInventoryHash: expectedInventoryHash, Confirm: true})
	requireMaintenanceStatus(t, trashApplyPath, status, raw, http.StatusOK)

	status, body, raw = postMaintenanceJSON(t, mux, handlers.BedroomPresetDryRunRoute, nil)
	requireMaintenanceStatus(t, handlers.BedroomPresetDryRunRoute, status, raw, http.StatusCreated)
	if bedroomPlan, ok := body["plan"].(map[string]any); !ok || bedroomPlan["plan_id"] == nil {
		t.Fatalf("bedroom preset response=%s", raw)
	}

	migrationDryRun := models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{{
		ActionID: "route_migration", SourcePath: "ir-signals/fixture.json", TargetKind: "command",
		RoomID: "fixture", DeviceID: "device", TargetID: "route_power",
		Decision: &models.MigrationDecision{RoomID: "fixture", DeviceID: "device", TargetID: "route_power"},
	}}}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.MigrationDryRunRoute, migrationDryRun)
	requireMaintenanceStatus(t, handlers.MigrationDryRunRoute, status, raw, http.StatusCreated)
	migrationPlan, ok := body["plan"].(map[string]any)
	if !ok {
		t.Fatalf("migration dry-run response=%s", raw)
	}
	migrationPlanID, _ := migrationPlan["plan_id"].(string)
	migrationPlanDigest, _ := migrationPlan["plan_digest"].(string)
	migrationActions, _ := migrationPlan["actions"].([]any)
	if migrationPlanID == "" || migrationPlanDigest == "" || len(migrationActions) != 1 {
		t.Fatalf("migration plan response=%s", raw)
	}
	migrationAction, _ := migrationActions[0].(map[string]any)
	migrationActionID, _ := migrationAction["action_id"].(string)

	backupCreate := models.BackupCreateRequest{PlanID: migrationPlanID, PlanDigest: migrationPlanDigest, ActionIDs: []string{migrationActionID}}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.BackupCreateRoute, backupCreate)
	requireMaintenanceStatus(t, handlers.BackupCreateRoute, status, raw, http.StatusCreated)
	createdBackupID, ok := body["backup_id"].(string)
	if !ok || createdBackupID == "" {
		t.Fatalf("backup create response=%s", raw)
	}

	rehearsalPath := "/api/controller/maintenance/backups/" + createdBackupID + "/restore/rehearsal"
	status, _, raw = postMaintenanceJSON(t, mux, rehearsalPath, nil)
	requireMaintenanceStatus(t, rehearsalPath, status, raw, http.StatusOK)

	migrationApply := models.MigrationApplyRequest{PlanID: migrationPlanID, PlanDigest: migrationPlanDigest, ActionIDs: []string{migrationActionID}, RehearsalBackupID: createdBackupID, Confirm: true}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.MigrationApplyRoute, migrationApply)
	requireMaintenanceStatus(t, handlers.MigrationApplyRoute, status, raw, http.StatusOK)
	migrationOperationID, ok := body["operation_id"].(string)
	if !ok || migrationOperationID == "" {
		t.Fatalf("migration apply response=%s", raw)
	}

	status, _, raw = postMaintenanceJSON(t, mux, handlers.MigrationVerifyRoute, models.MigrationVerifyRequest{OperationID: migrationOperationID})
	requireMaintenanceStatus(t, handlers.MigrationVerifyRoute, status, raw, http.StatusOK)

	legacyDryRun := models.LegacyIRCutoverDryRunRequest{MigrationOperationID: migrationOperationID, Sources: []models.LegacyIRSourceClassification{{Path: "ir-signals/fixture.json", Classification: "intentionally_skipped"}}}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.LegacyIRCutoverDryRunRoute, legacyDryRun)
	requireMaintenanceStatus(t, handlers.LegacyIRCutoverDryRunRoute, status, raw, http.StatusCreated)
	legacyPlan, ok := body["plan_id"].(string)
	if !ok || legacyPlan == "" {
		t.Fatalf("legacy dry-run response=%s", raw)
	}
	legacyDigest, ok := body["plan_digest"].(string)
	if !ok || legacyDigest == "" {
		t.Fatalf("legacy dry-run digest response=%s", raw)
	}

	legacyBackupRequest := models.BackupCreateRequest{PlanID: legacyPlan, PlanDigest: legacyDigest, ActionIDs: []string{"archive_legacy_ir_signals"}}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.BackupCreateRoute, legacyBackupRequest)
	requireMaintenanceStatus(t, handlers.BackupCreateRoute, status, raw, http.StatusCreated)
	legacyBackupID, ok := body["backup_id"].(string)
	if !ok || legacyBackupID == "" {
		t.Fatalf("legacy backup response=%s", raw)
	}
	legacyRestorePath := "/api/controller/maintenance/backups/" + legacyBackupID + "/restore/rehearsal"
	status, _, raw = postMaintenanceJSON(t, mux, legacyRestorePath, nil)
	requireMaintenanceStatus(t, legacyRestorePath, status, raw, http.StatusOK)
	legacyApply := models.LegacyIRCutoverApplyRequest{PlanID: legacyPlan, PlanDigest: legacyDigest, RehearsalBackupID: legacyBackupID, ConfirmSourcePath: "data/ir-signals", Confirm: true}
	status, _, raw = postMaintenanceJSON(t, mux, handlers.LegacyIRCutoverApplyRoute, legacyApply)
	requireMaintenanceStatus(t, handlers.LegacyIRCutoverApplyRoute, status, raw, http.StatusOK)

	status, body, raw = postMaintenanceJSON(t, mux, handlers.MigrationRollbackDryRunRoute, models.MigrationRollbackDryRunRequest{OperationID: migrationOperationID})
	requireMaintenanceStatus(t, handlers.MigrationRollbackDryRunRoute, status, raw, http.StatusCreated)
	rollbackPlan, ok := body["plan"].(map[string]any)
	if !ok {
		t.Fatalf("rollback dry-run response=%s", raw)
	}
	rollbackPlanID, _ := rollbackPlan["plan_id"].(string)
	rollbackDigest, _ := rollbackPlan["plan_digest"].(string)
	rollbackActions, _ := rollbackPlan["actions"].([]any)
	if rollbackPlanID == "" || rollbackDigest == "" || len(rollbackActions) != 1 {
		t.Fatalf("rollback plan response=%s", raw)
	}
	rollbackAction, _ := rollbackActions[0].(map[string]any)
	rollbackActionID, _ := rollbackAction["action_id"].(string)
	status, _, raw = postMaintenanceJSON(t, mux, handlers.MigrationRollbackApplyRoute, models.MigrationRollbackApplyRequest{PlanID: rollbackPlanID, PlanDigest: rollbackDigest, ActionIDs: []string{rollbackActionID}, Confirm: true})
	requireMaintenanceStatus(t, handlers.MigrationRollbackApplyRoute, status, raw, http.StatusOK)

	health := services.NewDataHealthService(repositories.NewDataHealthRepository(fixture.config.DataRoot))
	issues, err := health.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var invalidIssue models.CleanupIssueSelection
	for _, issue := range issues {
		if issue.ID == "invalid_json:rooms/fixture/devices/device/adapters/ir/commands/bad.json" {
			invalidIssue = models.CleanupIssueSelection{IssueID: issue.ID, BeforeHash: issue.Hash}
		}
	}
	if invalidIssue.IssueID == "" {
		t.Fatalf("invalid JSON issue missing: %+v", issues)
	}
	status, body, raw = postMaintenanceJSON(t, mux, handlers.CleanupDryRunRoute, models.CleanupDryRunRequest{Issues: []models.CleanupIssueSelection{invalidIssue}})
	requireMaintenanceStatus(t, handlers.CleanupDryRunRoute, status, raw, http.StatusCreated)
	cleanupPlanID, _ := body["plan_id"].(string)
	cleanupDigest, _ := body["plan_digest"].(string)
	cleanupActions, _ := body["actions"].([]any)
	if cleanupPlanID == "" || cleanupDigest == "" || len(cleanupActions) != 1 {
		t.Fatalf("cleanup dry-run response=%s", raw)
	}
	cleanupAction, _ := cleanupActions[0].(map[string]any)
	cleanupActionID, _ := cleanupAction["action_id"].(string)
	cleanupApply := models.CleanupApplyRequest{PlanID: cleanupPlanID, PlanDigest: cleanupDigest, Issues: []models.CleanupIssueSelection{invalidIssue}, ActionIDs: []string{cleanupActionID}, Confirm: true}
	status, _, raw = postMaintenanceJSON(t, mux, handlers.CleanupApplyRoute, cleanupApply)
	requireMaintenanceStatus(t, handlers.CleanupApplyRoute, status, raw, http.StatusOK)

}

func TestConfiguredMaintenanceCLIReadsConfiguredBackupRoot(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "cli")
	repository, err := configuredBackupRepository(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	service := services.NewBackupService(repository, nil)
	var output bytes.Buffer
	if err := runMaintenanceCLI([]string{"restore-dry-run", fixture.backupID}, service, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), fixture.backupID) {
		t.Fatalf("CLI output=%s", output.String())
	}
	output.Reset()
	if err := runMaintenanceCLI([]string{"restore-rehearsal", fixture.backupID}, service, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), fixture.backupID) || !strings.Contains(output.String(), "rehearsal_verified") {
		t.Fatalf("rehearsal CLI output=%s", output.String())
	}
}

type configuredTrashFinalDeleteFixture struct {
	config  appconfig.Config
	entryID string
	trash   *repositories.TrashRepository
}

func newConfiguredTrashFinalDeleteFixture(t *testing.T) configuredTrashFinalDeleteFixture {
	t.Helper()
	base := t.TempDir()
	dataRoot := filepath.Join(base, "data")
	stateRoot := filepath.Join(base, "maintenance")
	backupRoot := filepath.Join(base, "backups")
	trashRoot := filepath.Join(base, "trash")
	softwareRoot := filepath.Join(base, "software")
	webRoot := filepath.Join(softwareRoot, "web")
	for _, dir := range []string{filepath.Join(dataRoot, "legacy"), filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), backupRoot, trashRoot, softwareRoot, webRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	dataFile := filepath.Join(dataRoot, "legacy", "item.json")
	if err := os.WriteFile(dataFile, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := appconfig.Config{HTTPListen: "127.0.0.1:3000", DataRoot: dataRoot, MaintenanceRoot: stateRoot, BackupRoot: backupRoot, TrashRoot: trashRoot, TrashRetentionDays: 30, SoftwareRoot: softwareRoot, WebRoot: webRoot, SocketPath: "/run/web-controller/test.sock", PicoSerialPath: "/dev/serial/by-id/usb-test-if00"}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: stateRoot, RuntimeDataDir: dataRoot, RepositoryDir: softwareRoot, WebRootDir: webRoot, BackupDir: backupRoot, TrashDir: trashRoot})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{Dir: trashRoot, RuntimeDataDir: dataRoot, RepositoryDir: softwareRoot, WebRootDir: webRoot, StateDir: stateRoot, BackupDir: backupRoot, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Operations().Create(models.MaintenanceOperation{Version: 1, ID: "original_op", Revision: 1, StartedAt: time.Now().UTC().Add(-40 * 24 * time.Hour), Target: "legacy/item.json", Type: "delete", Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	const entryID = "final_entry"
	if _, err := trash.Move(repositories.TrashMoveRequest{EntryID: entryID, SourceRelativePath: "legacy/item.json", OriginalID: "item", TargetType: "legacy", Reason: "operator delete", OperationType: "delete", OperationID: "original_op"}); err != nil {
		t.Fatal(err)
	}
	entry, err := trash.Load(entryID)
	if err != nil {
		t.Fatal(err)
	}
	deleted := time.Now().UTC().Add(-40 * 24 * time.Hour)
	eligible := deleted.AddDate(0, 0, 30)
	entry.DeletedAt, entry.EligibleAfter = &deleted, &eligible
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashRoot, entryID, "metadata.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return configuredTrashFinalDeleteFixture{config: config, entryID: entryID, trash: trash}
}

func TestConfiguredTrashFinalDeleteCLIUsesConfiguredRoots(t *testing.T) {
	fixture := newConfiguredTrashFinalDeleteFixture(t)
	service, err := configuredTrashFinalDeleteService(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	var dryRunOutput bytes.Buffer
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete-dry-run", "--entry-id", fixture.entryID}, service, nil, &dryRunOutput); err != nil {
		t.Fatal(err)
	}
	var preview services.TrashFinalDeletePreview
	if err := json.Unmarshal(dryRunOutput.Bytes(), &preview); err != nil {
		t.Fatalf("dry-run output=%s err=%v", dryRunOutput.String(), err)
	}
	if preview.EntryID != fixture.entryID || !preview.Eligible || preview.PlanID == "" || preview.PlanDigest == "" {
		t.Fatalf("dry-run preview=%+v", preview)
	}
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete", "--plan-id", preview.PlanID}, service, nil, io.Discard); err == nil {
		t.Fatal("apply without controlling TTY unexpectedly succeeded")
	}
	wrongTTY := bytes.NewBufferString("wrong_entry\n")
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete", "--plan-id", preview.PlanID}, service, wrongTTY, io.Discard); !errors.Is(err, services.ErrTrashFinalDeleteConfirmation) {
		t.Fatalf("wrong confirmation error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.config.TrashRoot, fixture.entryID, "payload")); err != nil {
		t.Fatalf("payload changed after rejected confirmation: %v", err)
	}
	entry, err := fixture.trash.Load(fixture.entryID)
	if err != nil || entry.Status != "sealed" {
		t.Fatalf("Trash entry changed after rejected apply: %+v %v", entry, err)
	}
}

func TestConfiguredLoopbackHTTPAndDaemonSocketIntegration(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "step3")
	socketPath := filepath.Join(t.TempDir(), "controller.sock")
	unixListener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unixListener.Close()
	t.Cleanup(func() { _ = controller_client.ConfigureSocketPath("/run/web-controller/controller-daemon.sock") })
	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := unixListener.Accept()
		if acceptErr != nil {
			accepted <- acceptErr
			return
		}
		defer connection.Close()
		command, readErr := bufio.NewReader(connection).ReadString('\n')
		if readErr != nil {
			accepted <- readErr
			return
		}
		if strings.TrimSpace(command) != "LED_STATUS" {
			accepted <- errors.New("unexpected daemon command")
			return
		}
		_, writeErr := connection.Write([]byte("{\"ok\":true,\"device\":\"controller-daemon\"}\n"))
		accepted <- writeErr
	}()
	portProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpAddress := portProbe.Addr().String()
	if err := portProbe.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.config.SocketPath = socketPath
	fixture.config.HTTPListen = httpAddress
	mux := newHTTPMuxWithConfig(fixture.config)
	listener, err := listenConfiguredHTTP(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		t.Fatalf("HTTP listener address=%q host=%q err=%v", listener.Addr(), host, err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- http.Serve(listener, mux) }()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get("http://" + listener.Addr().String() + "/api/health/controller")
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"healthy"`) {
		_ = listener.Close()
		t.Fatalf("controller health status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	<-serveResult
}

func TestConfiguredLoopbackHTTPRemainsHealthyWhenDaemonSocketIsUnavailable(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "step3_daemon_unavailable")
	fixture.config.SocketPath = filepath.Join(t.TempDir(), "missing-controller.sock")
	fixture.config.HTTPListen = "127.0.0.1:0"
	t.Cleanup(func() { _ = controller_client.ConfigureSocketPath("/run/web-controller/controller-daemon.sock") })

	mux := newHTTPMuxWithConfig(fixture.config)
	listener, err := listenConfiguredHTTP(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- http.Serve(listener, mux) }()
	defer func() {
		_ = listener.Close()
		<-serveResult
	}()

	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	baseURL := "http://" + listener.Addr().String()

	response, err := client.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	var backendHealth models.HealthResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&backendHealth)
	_ = response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || !backendHealth.OK || backendHealth.Status != "healthy" {
		t.Fatalf("backend health status=%d response=%+v err=%v", response.StatusCode, backendHealth, decodeErr)
	}

	response, err = client.Get(baseURL + "/api/health/controller")
	if err != nil {
		t.Fatalf("GET /api/health/controller: %v", err)
	}
	var controllerHealth models.ControllerHealthResponse
	decodeErr = json.NewDecoder(response.Body).Decode(&controllerHealth)
	_ = response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusServiceUnavailable || controllerHealth.OK || controllerHealth.DaemonOK || controllerHealth.Status != "unhealthy" || controllerHealth.Error == "" {
		t.Fatalf("controller health status=%d response=%+v err=%v", response.StatusCode, controllerHealth, decodeErr)
	}
}

func TestConfiguredRootRoomDeleteMovesSourceToTrash(t *testing.T) {
	base := t.TempDir()
	dataRoot := filepath.Join(base, "data")
	roomsDir := filepath.Join(dataRoot, "rooms")
	stateRoot := filepath.Join(base, "state")
	trashRoot := filepath.Join(base, "trash")
	for _, dir := range []string{roomsDir, filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), trashRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR", stateRoot)
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", trashRoot)
	t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", "30")
	t.Setenv("WEB_CONTROLLER_BACKUP_DIR", "")
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })

	mux := newHTTPMuxWithDataRoot(roomsDir)
	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/controller/rooms", strings.NewReader(`{"id":"selected","name":"Selected"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(create, createRequest)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}

	remove := httptest.NewRecorder()
	mux.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/controller/rooms/selected", nil))
	if remove.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
	if _, err := os.Stat(filepath.Join(roomsDir, "selected", "room.json")); !os.IsNotExist(err) {
		t.Fatalf("room source still exists: %v", err)
	}
	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("Trash entries=%d, want 1", len(entries))
	}
}

func TestConfiguredMaintenanceRootsAreUsed(t *testing.T) {
	base := t.TempDir()
	dataRoot := filepath.Join(base, "data")
	stateRoot := filepath.Join(base, "maintenance")
	backupRoot := filepath.Join(base, "backups")
	trashRoot := filepath.Join(base, "trash")
	softwareRoot := filepath.Join(base, "software")
	webRoot := filepath.Join(softwareRoot, "web")
	for _, dir := range []string{dataRoot, filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), backupRoot, trashRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "WEB_CONTROLLER_BACKUP_DIR", "WEB_CONTROLLER_TRASH_DIR", "WEB_CONTROLLER_TRASH_RETENTION_DAYS"} {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { handlers.ConfigureDataRoot("") })

	mux := newHTTPMuxWithConfig(appconfig.Config{
		HTTPListen: "127.0.0.1:3000", DataRoot: dataRoot, MaintenanceRoot: stateRoot,
		BackupRoot: backupRoot, TrashRoot: trashRoot, TrashRetentionDays: 30,
		SoftwareRoot: softwareRoot, WebRoot: webRoot,
		SocketPath: "/run/web-controller/test.sock", PicoSerialPath: "/dev/serial/by-id/usb-test-if00",
	})
	for _, path := range []string{
		"/api/controller/maintenance/operations",
		"/api/controller/maintenance/trash",
		"/api/controller/maintenance/data-health",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestFinalDeleteHasNoHTTPRoute(t *testing.T) {
	t.Setenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", "")
	t.Setenv("WEB_CONTROLLER_BACKUP_DIR", "")
	mux := newHTTPMux()
	for _, path := range []string{
		"/api/controller/maintenance/trash-final-delete",
		"/api/controller/maintenance/trash/final-delete",
		"/api/controller/maintenance/trash/entry_1/final-delete",
		"/api/controller/maintenance/trash/entry_1/final-delete/dry-run",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, response.Code)
			}
		}
	}
}

func TestLegacyIRSignalRoutesRemoved(t *testing.T) {
	t.Setenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", "")
	t.Setenv("WEB_CONTROLLER_BACKUP_DIR", "")
	mux := newHTTPMux()
	for _, path := range []string{
		"/api/controller/rooms/bedroom/ir/signals",
		"/api/controller/rooms/bedroom/ir/signals/example",
		"/api/controller/rooms/bedroom/ir/signals/example/send",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, response.Code)
			}
		}
	}
}

func TestLegacyBedroomRoutesRemoved(t *testing.T) {
	t.Setenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", "")
	t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", "")
	t.Setenv("WEB_CONTROLLER_BACKUP_DIR", "")
	mux := newHTTPMux()
	paths := []string{
		"/api/controller/rooms/bedroom/light/on",
		"/api/controller/rooms/bedroom/light/full",
		"/api/controller/rooms/bedroom/light/off",
		"/api/controller/rooms/bedroom/light/night",
		"/api/controller/rooms/bedroom/light/bright/up",
		"/api/controller/rooms/bedroom/light/bright/down",
		"/api/controller/rooms/bedroom/light/status",
		"/api/controller/rooms/bedroom/ir/test",
		"/api/controller/rooms/bedroom/ir/capture",
		"/api/controller/rooms/bedroom/pico-led/on",
		"/api/controller/rooms/bedroom/pico-led/off",
		"/api/controller/rooms/bedroom/pico-led/status",
	}
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, response.Code)
			}
		}
	}
	_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/api/controller/rooms/bedroom/devices/roomlight/ir/commands", nil))
	if pattern != "/api/controller/rooms/" {
		t.Errorf("new IR Command route matched %q, want Room/Device router", pattern)
	}
}
