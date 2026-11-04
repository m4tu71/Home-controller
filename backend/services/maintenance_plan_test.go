package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
)

type maintenancePlanFixture struct {
	service  *MaintenancePlanService
	state    *repositories.MaintenanceState
	backup   *repositories.BackupRepository
	root     string
	stateDir string
}

func newMaintenancePlanFixture(t *testing.T) maintenancePlanFixture {
	t.Helper()
	base := t.TempDir()
	config := repositories.BackupConfig{Dir: filepath.Join(base, "backups"), RuntimeDataDir: filepath.Join(base, "data"), RepositoryDir: filepath.Join(base, "repo"), WebRootDir: filepath.Join(base, "web"), StateDir: filepath.Join(base, "state"), TrashDir: filepath.Join(base, "trash")}
	for _, dir := range []string{config.Dir, config.RuntimeDataDir, config.RepositoryDir, config.WebRootDir, config.StateDir, filepath.Join(config.StateDir, "plans"), filepath.Join(config.StateDir, "operations"), config.TrashDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config.RuntimeDataDir, "a.bin"), []byte("source-a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.RuntimeDataDir, "b.bin"), []byte("source-b"), 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := repositories.NewBackupRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: config.StateDir, RuntimeDataDir: config.RuntimeDataDir, RepositoryDir: config.RepositoryDir, WebRootDir: config.WebRootDir, BackupDir: config.Dir, TrashDir: config.TrashDir})
	if err != nil {
		t.Fatal(err)
	}
	service := NewMaintenancePlanService(state.Plans(), backup)
	service.newID = func() (string, error) { return "plan_0123456789abcdef0123456789abcdef", nil }
	service.now = func() time.Time { return time.Date(2026, 9, 24, 1, 2, 3, 0, time.FixedZone("JST", 9*60*60)) }
	return maintenancePlanFixture{service: service, state: state, backup: backup, root: config.RuntimeDataDir, stateDir: config.StateDir}
}

func planDraft() models.MaintenancePlanEnvelope {
	return models.MaintenancePlanEnvelope{
		OperationType:     "migration",
		PayloadSchemaID:   "migration.v1",
		Payload:           json.RawMessage(`{ "z": 1, "a": { "second": true, "first": 2 } }`),
		ReadyToApply:      true,
		BackupRequirement: "rehearsal_and_apply",
		Actions: []models.MaintenancePlanAction{
			{ID: "action_a", Action: "copy", Selectable: true, Destructive: true, BackupSources: []string{"a.bin"}},
			{ID: "action_b", Action: "skip", Selectable: false, BackupSources: []string{"b.bin"}},
		},
	}
}

func TestMaintenancePlanCreateCanonicalDigestAndSelection(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	plan, err := f.service.Create(planDraft())
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanID != "plan_0123456789abcdef0123456789abcdef" || plan.Version != models.MaintenancePlanVersion || plan.Digest == "" || plan.CreatedAt.Location() != time.UTC {
		t.Fatalf("issued envelope=%+v", plan)
	}
	if string(plan.Payload) != `{"a":{"first":2,"second":true},"z":1}` {
		t.Fatalf("payload was not canonicalized: %s", plan.Payload)
	}
	loaded, err := f.service.Load(plan.PlanID)
	if err != nil || loaded.Digest != plan.Digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	restarted, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: f.stateDir})
	if err != nil {
		t.Fatal(err)
	}
	restartedPlan, err := NewMaintenancePlanService(restarted.Plans(), f.backup).Load(plan.PlanID)
	if err != nil || restartedPlan.Digest != plan.Digest {
		t.Fatalf("restart load=%+v err=%v", restartedPlan, err)
	}
	selection, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"action_a"})
	if err != nil || len(selection.Sources) != 1 || selection.Sources[0] != "a.bin" || selection.FileCount != 1 || selection.TotalSize != int64(len("source-a")) {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
	if _, err := f.service.VerifyBackupSelection("plan_11111111111111111111111111111111", plan.Digest, []string{"action_a"}); !errors.Is(err, repositories.ErrMaintenanceMissing) {
		t.Fatalf("unknown plan error=%v", err)
	}
	if _, err := f.service.VerifyBackupSelection(plan.PlanID, "sha256:wrong", []string{"action_a"}); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("digest error=%v", err)
	}
	if _, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"unknown"}); !errors.Is(err, ErrMaintenancePlanActionMissing) {
		t.Fatalf("unknown action error=%v", err)
	}
	if _, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"action_b"}); !errors.Is(err, ErrMaintenancePlanNotReady) {
		t.Fatalf("unselectable action error=%v", err)
	}
	if _, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"action_a", "action_a"}); !errors.Is(err, ErrMaintenancePlanInvalid) {
		t.Fatalf("duplicate action error=%v", err)
	}
	if _, err := f.service.Create(planDraft()); !errors.Is(err, repositories.ErrMaintenanceDuplicate) {
		t.Fatalf("plan ID collision error=%v", err)
	}
}

func TestMaintenancePlanRejectsUnsupportedEnvelopeAndCorruptDigest(t *testing.T) {
	tests := []struct {
		name   string
		change func(*models.MaintenancePlanEnvelope)
	}{
		{"unknown operation", func(p *models.MaintenancePlanEnvelope) { p.OperationType = "unknown" }},
		{"unknown schema", func(p *models.MaintenancePlanEnvelope) { p.PayloadSchemaID = "migration.v99" }},
		{"invalid backup requirement", func(p *models.MaintenancePlanEnvelope) { p.BackupRequirement = "sometimes" }},
		{"caller supplied id", func(p *models.MaintenancePlanEnvelope) { p.PlanID = "plan_user" }},
		{"path traversal", func(p *models.MaintenancePlanEnvelope) { p.Actions[0].BackupSources = []string{"../a.bin"} }},
		{"operation requirement mismatch", func(p *models.MaintenancePlanEnvelope) { p.BackupRequirement = "apply" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMaintenancePlanFixture(t)
			draft := planDraft()
			tc.change(&draft)
			if _, err := f.service.Create(draft); !errors.Is(err, ErrMaintenancePlanInvalid) && !errors.Is(err, ErrMaintenancePlanUnsupported) {
				t.Fatalf("invalid plan error=%v", err)
			}
		})
	}
	f := newMaintenancePlanFixture(t)
	plan, err := f.service.Create(planDraft())
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(filepath.Dir(f.root), "state", "plans", plan.PlanID+".json")
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	stored["ready_to_apply"] = false
	tampered, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payloadPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Load(plan.PlanID); !errors.Is(err, repositories.ErrMaintenanceCorrupt) {
		t.Fatalf("tampered plan error=%v", err)
	}
	f2 := newMaintenancePlanFixture(t)
	missingPayloadPlan, err := f2.service.Create(planDraft())
	if err != nil {
		t.Fatal(err)
	}
	missingPayloadPath := filepath.Join(f2.stateDir, "plans", missingPayloadPlan.PlanID+".json")
	data, err = os.ReadFile(missingPayloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	delete(stored, "payload")
	data, err = json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missingPayloadPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f2.service.Load(missingPayloadPlan.PlanID); !errors.Is(err, repositories.ErrMaintenanceCorrupt) {
		t.Fatalf("missing payload error=%v", err)
	}
}

func TestMaintenancePlanRejectsStaleSourceBeforeBackup(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	plan, err := f.service.Create(planDraft())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "a.bin"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"action_a"}); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("stale source error=%v", err)
	}
}

func TestMaintenancePlanBackupAssociationVerifier(t *testing.T) {
	f := newMaintenancePlanFixture(t)
	plan, err := f.service.Create(planDraft())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := f.service.VerifyBackupSelection(plan.PlanID, plan.Digest, []string{"action_a"})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := f.backup.Create(repositories.BackupCreate{ID: "backup_1", Role: "rehearsal", PlanID: plan.PlanID, ActionIDs: []string{"action_a"}, Sources: selection.Sources, ExpectedInventoryHash: selection.InventoryHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.VerifyRehearsalBackup(plan.PlanID, plan.Digest, []string{"action_a"}, backup, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.VerifyRehearsalBackup(plan.PlanID, plan.Digest, []string{"action_a"}, backup, true); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("unverified rehearsal accepted: %v", err)
	}
	if _, err := f.backup.RestoreRehearsal(backup.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	verified, err := f.backup.Load(backup.Bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.VerifyRehearsalBackup(plan.PlanID, plan.Digest, []string{"action_a"}, verified, true); err != nil {
		t.Fatalf("verified rehearsal rejected: %v", err)
	}
	verified.Bundle.ActionIDs = []string{"action_b"}
	if err := f.service.VerifyRehearsalBackup(plan.PlanID, plan.Digest, []string{"action_a"}, verified, true); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("mismatched association error=%v", err)
	}
	apply, err := f.backup.Create(repositories.BackupCreate{ID: "backup_2", Role: "apply", PlanID: plan.PlanID, ActionIDs: []string{"action_a"}, Sources: selection.Sources, ExpectedInventoryHash: selection.InventoryHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.VerifyApplyBackup(plan.PlanID, plan.Digest, []string{"action_a"}, apply); err != nil {
		t.Fatalf("valid apply backup rejected: %v", err)
	}
	if err := f.service.VerifyRehearsalBackup(plan.PlanID, plan.Digest, []string{"action_a"}, apply, false); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("apply bundle accepted as rehearsal: %v", err)
	}
}
