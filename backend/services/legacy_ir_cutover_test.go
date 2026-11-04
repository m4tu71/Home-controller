package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
)

type fixedLegacyMigrationVerifier struct{ status string }

func (v fixedLegacyMigrationVerifier) Verify(models.MigrationVerifyRequest) (models.MigrationVerifyResult, error) {
	return models.MigrationVerifyResult{Status: v.status, Targets: []models.MigrationVerifyTarget{{Status: v.status}}}, nil
}

func legacyCutoverFixture(t *testing.T) (cleanupApplyFixture, *LegacyIRCutoverPlanService, *BackupService, models.LegacyIRCutoverDryRunRequest) {
	t.Helper()
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/valid.json", `{"name":"valid","room":"bedroom","device":"ac","format":"raw","carrier_khz":38}`)
	f.put(t, "ir-signals/empty.json", "")
	f.put(t, "rooms/bedroom/room.json", `{"id":"bedroom","name":"Bedroom"}`)
	op := models.MaintenanceOperation{Version: 1, ID: "migration_fixture", Revision: 1, StartedAt: time.Now().UTC(), Target: "migration", Type: "migration", Status: "verified", RehearsalBackupID: "rehearsal_fixture", ApplyBackupID: "apply_fixture", Actions: []models.MaintenanceOperationAction{{ID: "preset", Action: "write_ir_command", Status: "verified"}}}
	if err := f.state.Operations().Create(op); err != nil {
		t.Fatal(err)
	}
	plans := NewMaintenancePlanService(f.state.Plans(), f.backup)
	service := NewLegacyIRCutoverPlanService(f.root, plans, f.state.Operations(), fixedLegacyMigrationVerifier{status: "verified"})
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(plans)
	request := models.LegacyIRCutoverDryRunRequest{MigrationOperationID: op.ID, Sources: []models.LegacyIRSourceClassification{{Path: "ir-signals/valid.json", Classification: "intentionally_skipped"}, {Path: "ir-signals/empty.json", Classification: "invalid"}}}
	return f, service, backup, request
}

func TestLegacyIRCutoverPlanRequiresExactClassificationsAndVerifiedMigration(t *testing.T) {
	f, service, _, request := legacyCutoverFixture(t)
	plan, err := service.Create(request)
	if err != nil || plan.PayloadSchemaID != "legacy_ir_cutover.v1" || plan.BackupRequirement != "rehearsal_and_apply" || len(plan.Actions) != 1 || plan.Actions[0].BackupFileCount != 2 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*models.LegacyIRCutoverDryRunRequest)
	}{
		{"missing", func(r *models.LegacyIRCutoverDryRunRequest) { r.Sources = r.Sources[:1] }},
		{"wrong_status", func(r *models.LegacyIRCutoverDryRunRequest) { r.Sources[0].Classification = "invalid" }},
		{"duplicate", func(r *models.LegacyIRCutoverDryRunRequest) { r.Sources[1] = r.Sources[0] }},
		{"migrated_not_approved", func(r *models.LegacyIRCutoverDryRunRequest) { r.Sources[0].Classification = "migrated" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := request
			changed.Sources = append([]models.LegacyIRSourceClassification(nil), request.Sources...)
			tc.change(&changed)
			if _, err := service.Create(changed); err == nil {
				t.Fatal("unsafe classification accepted")
			}
		})
	}
	service.verify = fixedLegacyMigrationVerifier{status: "failed"}
	if _, err := service.Create(request); !errors.Is(err, ErrLegacyIRCutoverStale) {
		t.Fatalf("failed migration verify: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "ir-signals", "valid.json")); err != nil {
		t.Fatalf("dry-run changed runtime: %v", err)
	}
}

func TestLegacyIRCutoverRehearsalApplyTrashAndRestorePreview(t *testing.T) {
	f, plans, backup, request := legacyCutoverFixture(t)
	plan, err := plans.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{legacyIRCutoverActionID}})
	if err != nil || rehearsal.Registry.State != "snapshot_verified" || rehearsal.Bundle.FileCount != 2 {
		t.Fatalf("rehearsal=%+v err=%v", rehearsal, err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	apply := NewLegacyIRCutoverApplyService(plans, f.state.Operations(), backup, f.trash, f.root, f.trashRoot)
	op, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: rehearsal.Bundle.ID, ConfirmSourcePath: "data/ir-signals", Confirm: true})
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || op.RehearsalBackupID != rehearsal.Bundle.ID {
		t.Fatalf("operation=%+v err=%v", op, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "ir-signals")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old runtime path still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "rooms", "bedroom", "room.json")); err != nil {
		t.Fatalf("unrelated Room changed: %v", err)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 1 || entries[0].Status != "sealed" || !entries[0].RestoreAvailable || entries[0].BeforeInventory.FileCount != 2 || entries[0].OperationID != op.ID {
		t.Fatalf("Trash entries=%+v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(f.trashRoot, entries[0].ID, "payload", "valid.json")); err != nil {
		t.Fatalf("Trash payload missing: %v", err)
	}
	preview, err := f.trash.PreviewRestore(entries[0].ID)
	if err != nil || !preview.RestoreAvailable || !preview.PathAvailable || !preview.IDAvailable {
		t.Fatalf("restore preview=%+v err=%v", preview, err)
	}
	if _, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: rehearsal.Bundle.ID, ConfirmSourcePath: "data/ir-signals", Confirm: true}); err == nil {
		t.Fatal("same plan applied twice")
	}
	restored, err := f.trash.Restore(entries[0].ID, preview.ExpectedInventoryHash, "restore_fixture")
	if err != nil || restored.Status != "restored" {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "ir-signals", "valid.json")); err != nil {
		t.Fatalf("legacy directory restore failed: %v", err)
	}
}

func TestLegacyIRCutoverRejectsChangedSourceAndUnverifiedBackup(t *testing.T) {
	f, plans, backup, request := legacyCutoverFixture(t)
	plan, err := plans.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	apply := NewLegacyIRCutoverApplyService(plans, f.state.Operations(), backup, f.trash, f.root, f.trashRoot)
	if _, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: "missing", Confirm: true}); err == nil {
		t.Fatal("missing exact target confirmation accepted")
	}
	if _, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: "missing", ConfirmSourcePath: "data/ir-signals", Confirm: true}); err == nil {
		t.Fatal("missing rehearsal accepted")
	}
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{legacyIRCutoverActionID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: rehearsal.Bundle.ID, ConfirmSourcePath: "data/ir-signals", Confirm: true}); err == nil {
		t.Fatal("unrestored rehearsal accepted")
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	f.put(t, "ir-signals/valid.json", `{"name":"changed"}`)
	if _, err := apply.Apply(models.LegacyIRCutoverApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: rehearsal.Bundle.ID, ConfirmSourcePath: "data/ir-signals", Confirm: true}); err == nil {
		t.Fatal("changed source accepted")
	}
	if _, err := os.Stat(filepath.Join(f.root, "ir-signals", "valid.json")); err != nil {
		t.Fatalf("rejected apply changed runtime: %v", err)
	}
}

func TestLegacyIRCutoverRejectsSymlinkAndAdditionalSource(t *testing.T) {
	f, plans, _, request := legacyCutoverFixture(t)
	f.put(t, "ir-signals/extra.json", `{"name":"extra"}`)
	if _, err := plans.Create(request); err == nil {
		t.Fatal("unclassified extra source accepted")
	}
	linked := filepath.Join(f.root, "ir-signals", "linked.json")
	if err := os.Symlink(filepath.Join(f.root, "ir-signals", "valid.json"), linked); err != nil {
		t.Fatal(err)
	}
	request.Sources = append(request.Sources, models.LegacyIRSourceClassification{Path: "ir-signals/extra.json", Classification: "intentionally_skipped"}, models.LegacyIRSourceClassification{Path: "ir-signals/linked.json", Classification: "intentionally_skipped"})
	if _, err := plans.Create(request); err == nil {
		t.Fatal("symlink source accepted")
	}
}

var _ legacyIRCutoverOperationReader = (*repositories.MaintenanceOperationRepository)(nil)
