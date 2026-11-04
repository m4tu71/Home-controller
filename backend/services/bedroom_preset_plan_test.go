package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestBedroomPresetSourceLessPlanApplyVerifyAndRollbackPreview(t *testing.T) {
	f := newCleanupApplyFixture(t)
	if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
		t.Fatal(err)
	}
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	planner := NewBedroomPresetPlanService(f.root, migration)
	plan, err := planner.Create()
	if err != nil || plan.PayloadSchemaID != "migration.v2" || !plan.ReadyToApply || len(plan.Actions) != 6 {
		t.Fatalf("plan schema=%s ready=%v actions=%d err=%v", plan.PayloadSchemaID, plan.ReadyToApply, len(plan.Actions), err)
	}
	var payload models.MigrationPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for _, item := range payload.Actions {
		if item.SourceKind != bedroomPresetSourceKind || item.Source != (models.MigrationPathSnapshot{}) || item.Decision == nil || item.Decision.TargetName == "" {
			t.Fatalf("source-less item=%+v", item)
		}
	}
	if _, err := os.Lstat(filepath.Join(f.root, "rooms/bedroom")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run mutated target: %v", err)
	}
	ids := make([]string, 0, 6)
	for _, action := range plan.Actions {
		ids = append(ids, action.ID)
		if len(action.BackupSources) != 1 || action.BackupSources[0] != "rooms" {
			t.Fatalf("backup sources=%v", action.BackupSources)
		}
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if rehearsal.Bundle.FileCount != 0 || len(rehearsal.Bundle.Directories) != 1 || rehearsal.Bundle.Directories[0] != "rooms" {
		t.Fatalf("rehearsal=%+v", rehearsal.Bundle)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	apply := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root)
	request := models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true}
	op, err := apply.Apply(request)
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || len(op.Actions) != 6 {
		t.Fatalf("op status=%s error=%s actions=%d err=%v", op.Status, op.Error, len(op.Actions), err)
	}
	verified, err := NewMigrationVerifyService(migration, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || verified.Status != "verified" || len(verified.Targets) != 6 {
		t.Fatalf("verify status=%s targets=%d err=%v", verified.Status, len(verified.Targets), err)
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(f.root, "rooms"))
	for _, spec := range bedroomPresetSpecs {
		command, err := repo.LoadCommand("bedroom", "roomlight", spec.ID)
		if err != nil || command.Name != spec.Name || command.PicoCommand != spec.Pico {
			t.Fatalf("command id=%s value=%+v err=%v", spec.ID, command, err)
		}
	}
	rollback := NewMigrationRollbackPlanService(common, migration, f.state.Operations(), backup, f.root)
	preview, err := rollback.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || !preview.ReadyToApply || len(preview.Actions) != 6 {
		t.Fatalf("rollback ready=%v actions=%d err=%v", preview.ReadyToApply, len(preview.Actions), err)
	}
	if _, err := apply.Apply(request); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("repeat err=%v", err)
	}
	rollbackIDs := make([]string, 0, len(preview.Actions))
	for _, action := range preview.Actions {
		if action.Selectable {
			rollbackIDs = append(rollbackIDs, action.ID)
		}
	}
	rollbackApply := NewMigrationRollbackApplyService(rollback, f.state.Operations(), backup, f.trash, f.root, f.trashRoot)
	rolled, err := rollbackApply.Apply(models.MigrationRollbackApplyRequest{PlanID: preview.PlanID, PlanDigest: preview.Digest, ActionIDs: rollbackIDs, Confirm: true})
	if err != nil || rolled.Status != "verified" || len(rolled.Actions) != 6 {
		t.Fatalf("rollback status=%s actions=%d err=%v", rolled.Status, len(rolled.Actions), err)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 6 {
		t.Fatalf("Trash entries=%d err=%v", len(entries), err)
	}
	roomPath := filepath.Join(f.root, "rooms/bedroom/room.json")
	roomBytes, err := os.ReadFile(roomPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, []byte(`{"id":"bedroom","name":"Other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := planner.Create(); !errors.Is(err, ErrBedroomPresetUnavailable) {
		t.Fatalf("changed scaffold err=%v", err)
	}
	if err := os.WriteFile(roomPath, roomBytes, 0600); err != nil {
		t.Fatal(err)
	}
	recutover, err := planner.Create()
	if err != nil || !recutover.ReadyToApply || recutover.PlanID == plan.PlanID {
		t.Fatalf("recutover plan=%+v err=%v", recutover, err)
	}
	var recutoverPayload models.MigrationPlanPayload
	if err := json.Unmarshal(recutover.Payload, &recutoverPayload); err != nil {
		t.Fatal(err)
	}
	for _, item := range recutoverPayload.Actions {
		if !item.TargetParentBefore.Exists || len(item.PlannedWrites) != 1 || item.PlannedWrites[0].Path != item.TargetBefore.Path {
			t.Fatalf("recutover item=%+v", item)
		}
	}
	recutoverBackup, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: recutover.PlanID, PlanDigest: recutover.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(recutoverBackup.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roomPath, []byte(`{"id":"bedroom","name":"Other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Apply(models.MigrationApplyRequest{PlanID: recutover.PlanID, PlanDigest: recutover.Digest, ActionIDs: ids, RehearsalBackupID: recutoverBackup.Bundle.ID, Confirm: true}); err == nil {
		t.Fatal("stale scaffold apply succeeded")
	}
	if err := os.WriteFile(roomPath, roomBytes, 0600); err != nil {
		t.Fatal(err)
	}
	recutoverOp, err := apply.Apply(models.MigrationApplyRequest{PlanID: recutover.PlanID, PlanDigest: recutover.Digest, ActionIDs: ids, RehearsalBackupID: recutoverBackup.Bundle.ID, Confirm: true})
	if err != nil || recutoverOp.Status != "verified" {
		t.Fatalf("recutover status=%s err=%v", recutoverOp.Status, err)
	}
	if _, err := NewMigrationVerifyService(migration, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: recutoverOp.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestBedroomPresetSourceLessPlanRejectsMissingAnchorAndExistingTarget(t *testing.T) {
	f := newCleanupApplyFixture(t)
	service := NewBedroomPresetPlanService(f.root, NewMigrationPlanService(NewMaintenancePlanService(f.state.Plans(), f.backup)))
	if _, err := service.Create(); !errors.Is(err, ErrBedroomPresetUnavailable) {
		t.Fatalf("missing anchor err=%v", err)
	}
	if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, "rooms/bedroom"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(); !errors.Is(err, ErrBedroomPresetUnavailable) {
		t.Fatalf("existing bedroom err=%v", err)
	}
}

func TestBedroomPresetSourceLessPlanRejectsStaleAndPartialSelection(t *testing.T) {
	for _, change := range []string{"anchor_changed", "target_collision", "partial_selection"} {
		t.Run(change, func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
				t.Fatal(err)
			}
			common := NewMaintenancePlanService(f.state.Plans(), f.backup)
			migration := NewMigrationPlanService(common)
			plan, err := NewBedroomPresetPlanService(f.root, migration).Create()
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, 6)
			for _, a := range plan.Actions {
				ids = append(ids, a.ID)
			}
			switch change {
			case "anchor_changed":
				if err := os.WriteFile(filepath.Join(f.root, "rooms/new.json"), []byte(`{"unexpected":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "target_collision":
				p := filepath.Join(f.root, "rooms/bedroom/devices/roomlight/adapters/ir/commands/on_ch3.json")
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(`{"id":"on_ch3"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
			if change != "partial_selection" {
				if _, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids}); !errors.Is(err, ErrMaintenancePlanStale) {
					t.Fatalf("changed inventory err=%v", err)
				}
				return
			}
			rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
				t.Fatal(err)
			}
			request := models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids[:1], RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true}
			if _, err := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root).Apply(request); !errors.Is(err, ErrMigrationApplyInvalid) {
				t.Fatalf("partial selection err=%v", err)
			}
		})
	}
}

func TestBedroomPresetSourceLessConcurrentPlansSingleClaim(t *testing.T) {
	f := newCleanupApplyFixture(t)
	if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
		t.Fatal(err)
	}
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	planner := NewBedroomPresetPlanService(f.root, migration)
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	requests := make([]models.MigrationApplyRequest, 2)
	for i := range requests {
		plan, err := planner.Create()
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, 6)
		for _, a := range plan.Actions {
			ids = append(ids, a.ID)
		}
		rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
			t.Fatal(err)
		}
		requests[i] = models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true}
	}
	service := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range requests {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = service.Apply(requests[i]) }(i)
	}
	wg.Wait()
	succeeded, rejected := 0, 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, repositories.ErrMaintenanceTargetClaimed) || errors.Is(err, ErrMaintenancePlanStale) || errors.Is(err, ErrMigrationApplyStale) {
			rejected++
		} else {
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("errs=%v", errs)
	}
	operations, err := f.state.Operations().List()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, op := range operations {
		if op.Type == "migration" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("migration operations=%d", count)
	}
}

func TestBedroomPresetSourceLessApplyRejectsChangeAfterRehearsal(t *testing.T) {
	f := newCleanupApplyFixture(t)
	if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
		t.Fatal(err)
	}
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	plan, err := NewBedroomPresetPlanService(f.root, migration).Create()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 6)
	for _, a := range plan.Actions {
		ids = append(ids, a.ID)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "rooms", "changed.json"), []byte(`{"changed":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	request := models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true}
	if _, err := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root).Apply(request); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("stale apply err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "rooms/bedroom")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target created after stale plan: %v", err)
	}
}
