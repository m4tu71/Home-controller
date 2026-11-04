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

func rollbackFixture(t *testing.T) (cleanupApplyFixture, *MigrationRollbackPlanService, models.MaintenanceOperation) {
	t.Helper()
	f, _, op := verifyAppliedFixture(t, "raw")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	s := NewMigrationRollbackPlanService(common, NewMigrationPlanService(common), f.state.Operations(), NewBackupService(f.backup, f.state.Operations()).WithPlanService(common), f.root)
	return f, s, op
}

func rollbackPayload(t *testing.T, plan models.MaintenancePlanEnvelope) models.MigrationRollbackPlanPayload {
	t.Helper()
	var payload models.MigrationRollbackPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestMigrationRollbackDryRunCandidateSavedAndRuntimeUnchanged(t *testing.T) {
	f, s, op := rollbackFixture(t)
	before, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || !first.ReadyToApply || len(first.Actions) != 1 || !first.Actions[0].Selectable {
		t.Fatalf("plan=%+v err=%v", first, err)
	}
	p := rollbackPayload(t, first)
	if p.MigrationOperationID != op.ID || p.ApplyBackupID != op.ApplyBackupID || len(p.Targets) != 1 || !p.Targets[0].Eligible || p.Targets[0].Reason != "eligible" || p.Targets[0].ApplySHA256 != p.Targets[0].CurrentSHA256 || p.Targets[0].TargetPath != "rooms/r/devices/d/adapters/ir/commands/power.json" || len(p.Targets[0].Dependencies) == 0 {
		t.Fatalf("payload=%+v", p)
	}
	if len(first.Actions[0].BackupFiles) != 1 || first.Actions[0].BackupFiles[0].SHA256 != p.Targets[0].CurrentSHA256 {
		t.Fatalf("candidate backup inventory=%+v", first.Actions[0])
	}
	identityShown := false
	for _, dep := range p.Targets[0].Dependencies {
		if dep.Role == "ir_config" && !dep.CreatedByApply && dep.Path == "rooms/r/devices/d/adapters/ir/ir_config.json" {
			identityShown = true
		}
	}
	if !identityShown {
		t.Fatalf("existing IR dependency missing: %+v", p.Targets[0].Dependencies)
	}
	loaded, err := s.Load(first.PlanID)
	if err != nil || loaded.Digest != first.Digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	second, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || second.PlanID == first.PlanID || second.Digest == first.Digest {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	p2 := rollbackPayload(t, second)
	if !reflect.DeepEqual(p.Targets, p2.Targets) {
		t.Fatalf("double dry-run changed candidates: %+v %+v", p.Targets, p2.Targets)
	}
	after, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("runtime changed: %v", err)
	}
}

func TestMigrationRollbackDryRunExcludesHumanChangeAndExistingBefore(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, cleanupApplyFixture, models.MaintenanceOperation)
		reason string
	}{
		{"modified", func(t *testing.T, f cleanupApplyFixture, op models.MaintenanceOperation) {
			p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}, "modified_target"},
		{"missing", func(t *testing.T, f cleanupApplyFixture, op models.MaintenanceOperation) {
			if err := os.Remove(filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")); err != nil {
				t.Fatal(err)
			}
		}, "missing_target"},
		{"existing_before", func(t *testing.T, f cleanupApplyFixture, op models.MaintenanceOperation) {
			stored, err := f.state.Operations().Load(op.ID)
			if err != nil {
				t.Fatal(err)
			}
			var snap models.MaintenanceSnapshot
			if json.Unmarshal(stored.Actions[0].BeforeState, &snap) != nil {
				t.Fatal("snapshot")
			}
			snap.Runtime.Exists = true
			snap.Runtime.Kind = "file"
			snap.Runtime.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			stored.Actions[0].BeforeState, _ = json.Marshal(snap)
			stored.Revision++
			if err := f.state.Operations().Update(stored); err != nil {
				t.Fatal(err)
			}
		}, "provenance_unconfirmed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, op := rollbackFixture(t)
			tc.mutate(t, f, op)
			plan, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
			if err != nil || plan.ReadyToApply {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			p := rollbackPayload(t, plan)
			if p.Targets[0].Eligible || p.Targets[0].Reason != tc.reason || len(plan.Actions[0].BackupSources) != 0 {
				t.Fatalf("target=%+v", p.Targets[0])
			}
		})
	}
}

func TestMigrationRollbackDryRunRejectsWrongOperationAndSymlink(t *testing.T) {
	f, s, op := rollbackFixture(t)
	if _, err := s.Create(models.MigrationRollbackDryRunRequest{}); !errors.Is(err, ErrMigrationRollbackInvalid) {
		t.Fatalf("invalid err=%v", err)
	}
	if _, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: "migration_missing"}); !errors.Is(err, repositories.ErrMaintenanceMissing) {
		t.Fatalf("missing err=%v", err)
	}
	p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.root, "ir-signals/raw.json"), p); err != nil {
		t.Fatal(err)
	}
	plan, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || plan.ReadyToApply {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if target := rollbackPayload(t, plan).Targets[0]; target.Eligible || target.Reason != "unsafe_or_unreadable_target" {
		t.Fatalf("target=%+v", target)
	}
}

func TestMigrationRollbackDryRunPartialApplyOnlyCreatedTarget(t *testing.T) {
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	writeMigrationFixture(t, f.root, "raw_median")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	first := migrationDryRunRequest("raw").Mappings[0]
	second := migrationDryRunRequest("raw_median").Mappings[0]
	second.ActionID = "migrate_second"
	second.TargetID = "second"
	second.Decision.TargetID = "second"
	second.Decision.ApproveRawMedian = true
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
	apply := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root)
	apply.writer.Writer = &migrationFailNthWriter{failAt: 2}
	op, err := apply.Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err == nil || op.Status != "partial_failure" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	rollback := NewMigrationRollbackPlanService(common, migration, f.state.Operations(), backup, f.root)
	preview, err := rollback.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || !preview.ReadyToApply {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	p := rollbackPayload(t, preview)
	byID := map[string]models.MigrationRollbackTarget{}
	for _, target := range p.Targets {
		byID[target.ActionID] = target
	}
	if !byID[first.ActionID].Eligible || byID[second.ActionID].Eligible || byID[second.ActionID].Reason != "missing_target" {
		t.Fatalf("targets=%+v", byID)
	}
}

func TestMigrationRollbackDryRunSavedPlanStaysFixedAfterEdit(t *testing.T) {
	f, s, op := rollbackFixture(t)
	first, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || !first.ReadyToApply {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(models.MigrationRollbackDryRunRequest{OperationID: op.ID})
	if err != nil || second.ReadyToApply || rollbackPayload(t, second).Targets[0].Reason != "modified_target" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	stored, err := s.Load(first.PlanID)
	if err != nil || stored.Digest != first.Digest || !rollbackPayload(t, stored).Targets[0].Eligible {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	after, err := repositories.NewDataHealthRepository(f.root).Scan()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("dry-run changed runtime: %v", err)
	}
}
