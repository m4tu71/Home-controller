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

func rollbackApplyFixture(t *testing.T) (cleanupApplyFixture, *MigrationRollbackPlanService, *MigrationRollbackApplyService, models.MaintenancePlanEnvelope) {
	t.Helper()
	f, plans, migrationOp := rollbackFixture(t)
	plan, err := plans.Create(models.MigrationRollbackDryRunRequest{OperationID: migrationOp.ID})
	if err != nil {
		t.Fatal(err)
	}
	common := plans.plans
	service := NewMigrationRollbackApplyService(plans, f.state.Operations(), NewBackupService(f.backup, f.state.Operations()).WithPlanService(common), f.trash, f.root, f.trashRoot)
	return f, plans, service, plan
}

func rollbackRequest(plan models.MaintenancePlanEnvelope) models.MigrationRollbackApplyRequest {
	ids := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		if action.Selectable {
			ids = append(ids, action.ID)
		}
	}
	return models.MigrationRollbackApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, Confirm: true}
}

func TestMigrationRollbackApplyMovesUnchangedTargetAndKeepsSource(t *testing.T) {
	f, _, service, plan := rollbackApplyFixture(t)
	request := rollbackRequest(plan)
	target := rollbackPayload(t, plan).Targets[0].TargetPath
	beforeSource, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.Apply(request)
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || len(op.Actions) != 1 {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(target))); !os.IsNotExist(err) {
		t.Fatalf("target still exists: %v", err)
	}
	afterSource, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json"))
	if err != nil || string(beforeSource) != string(afterSource) {
		t.Fatalf("source changed: %v", err)
	}
	var intended models.MaintenanceSnapshot
	if err := json.Unmarshal(op.Actions[0].IntendedState, &intended); err != nil {
		t.Fatal(err)
	}
	entryID := filepath.Dir(intended.Trash.Path)
	entry, err := f.trash.Load(entryID)
	if err != nil || entry.Status != "sealed" || entry.OriginalRelativePath != target || entry.OperationID != op.ID {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	if _, err := os.Stat(filepath.Join(f.trashRoot, filepath.FromSlash(intended.Trash.Path))); err != nil {
		t.Fatalf("payload missing: %v", err)
	}
	backup, err := f.backup.Load(op.ApplyBackupID)
	if err != nil || backup.Registry.Role != "apply" || backup.Registry.State != "point_in_time_verified" {
		t.Fatalf("backup=%+v err=%v", backup, err)
	}
	if _, err := service.Apply(request); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("repeat err=%v", err)
	}
}

func TestMigrationRollbackApplyRejectsChangedTargetAndInvalidSelection(t *testing.T) {
	f, _, service, plan := rollbackApplyFixture(t)
	request := rollbackRequest(plan)
	if _, err := service.Apply(models.MigrationRollbackApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: request.ActionIDs}); !errors.Is(err, ErrMigrationRollbackInvalid) {
		t.Fatalf("confirm err=%v", err)
	}
	request.PlanDigest = "wrong"
	if _, err := service.Apply(request); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("digest err=%v", err)
	}
	request.PlanDigest = plan.Digest
	target := rollbackPayload(t, plan).Targets[0].TargetPath
	path := filepath.Join(f.root, filepath.FromSlash(target))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(request); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("changed err=%v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(append(data, '\n')) {
		t.Fatalf("human change lost: %v", err)
	}
	operations, err := f.state.Operations().List()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		if op.Type == "migration_rollback" {
			t.Fatal("stale target created rollback operation")
		}
	}
}

func TestMigrationRollbackApplyCrossPlanConcurrentClaim(t *testing.T) {
	f, plans, service, first := rollbackApplyFixture(t)
	payload := rollbackPayload(t, first)
	second, err := plans.Create(models.MigrationRollbackDryRunRequest{OperationID: payload.MigrationOperationID})
	if err != nil {
		t.Fatal(err)
	}
	requests := []models.MigrationRollbackApplyRequest{rollbackRequest(first), rollbackRequest(second)}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range requests {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = service.Apply(requests[i]) }(i)
	}
	wg.Wait()
	succeeded, claimed := 0, 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if errors.Is(err, repositories.ErrMaintenanceTargetClaimed) || errors.Is(err, ErrMaintenancePlanStale) {
			claimed++
		} else {
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if succeeded != 1 || claimed != 1 {
		t.Fatalf("errs=%v", errs)
	}
	operations, err := f.state.Operations().List()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, op := range operations {
		if op.Type == "migration_rollback" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("rollback operations=%d", count)
	}
}

func TestMigrationRollbackApplyBackupAndTrashFailures(t *testing.T) {
	for _, stage := range []string{"backup", "trash"} {
		t.Run(stage, func(t *testing.T) {
			f, _, service, plan := rollbackApplyFixture(t)
			request := rollbackRequest(plan)
			target := rollbackPayload(t, plan).Targets[0].TargetPath
			before, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(target)))
			if err != nil {
				t.Fatal(err)
			}
			if stage == "backup" {
				service.backup = failingCleanupBackup{err: errors.New("injected backup failure")}
			} else {
				service.trash = &failingCleanupTrash{base: f.trash, failAt: 1, err: repositories.ErrTrashCrossDevice}
			}
			op, err := service.Apply(request)
			if err == nil || op.Status != "partial_failure" {
				t.Fatalf("op=%+v err=%v", op, err)
			}
			after, readErr := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(target)))
			if readErr != nil || string(after) != string(before) {
				t.Fatalf("target changed: %v", readErr)
			}
			if _, retryErr := service.Apply(request); !errors.Is(retryErr, repositories.ErrMaintenancePlanClaimed) {
				t.Fatalf("retry err=%v", retryErr)
			}
		})
	}
}

func TestMigrationRollbackApplySecondTargetFailurePreservesFirstTrash(t *testing.T) {
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	writeMigrationFixture(t, f.root, "raw_median")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	first := migrationDryRunRequest("raw").Mappings[0]
	second := migrationDryRunRequest("raw_median").Mappings[0]
	second.ActionID, second.TargetID, second.Decision.TargetID, second.Decision.ApproveRawMedian = "migrate_second", "second", "second", true
	plan, err := NewMigrationDryRunService(f.root, migration).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	ids := []string{first.ActionID, second.ActionID}
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	migrationOp, err := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root).Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err != nil || migrationOp.Status != "verified" {
		t.Fatalf("migration op=%+v err=%v", migrationOp, err)
	}
	rollbackPlans := NewMigrationRollbackPlanService(common, migration, f.state.Operations(), backup, f.root)
	rollbackPlan, err := rollbackPlans.Create(models.MigrationRollbackDryRunRequest{OperationID: migrationOp.ID})
	if err != nil {
		t.Fatal(err)
	}
	request := rollbackRequest(rollbackPlan)
	if len(request.ActionIDs) != 2 {
		t.Fatalf("selected=%v", request.ActionIDs)
	}
	service := NewMigrationRollbackApplyService(rollbackPlans, f.state.Operations(), backup, f.trash, f.root, f.trashRoot)
	service.trash = &failingCleanupTrash{base: f.trash, failAt: 2, err: repositories.ErrTrashCrossDevice}
	op, err := service.Apply(request)
	if err == nil || op.Status != "partial_failure" || op.ApplyBackupID == "" {
		t.Fatalf("rollback op=%+v err=%v", op, err)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("Trash entries=%+v err=%v", entries, err)
	}
	remaining := 0
	for _, target := range rollbackPayload(t, rollbackPlan).Targets {
		if _, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(target.TargetPath))); err == nil {
			remaining++
		}
	}
	if remaining != 1 {
		t.Fatalf("remaining targets=%d", remaining)
	}
	if _, err := service.Apply(request); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("retry err=%v", err)
	}
}
