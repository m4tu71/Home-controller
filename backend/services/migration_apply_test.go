package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"backend/models"
	"backend/repositories"
)

func migrationApplyFixture(t *testing.T) (cleanupApplyFixture, *MigrationApplyService, models.MigrationApplyRequest) {
	t.Helper()
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	migration := NewMigrationPlanService(common)
	plan, err := NewMigrationDryRunService(f.root, migration).Create(migrationDryRunRequest("raw"))
	if err != nil {
		t.Fatal(err)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{"migrate_power"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	service := NewMigrationApplyService(migration, f.state.Operations(), backup, f.root)
	request := models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{"migrate_power"}, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true}
	return f, service, request
}

func TestMigrationApplyCreatesOnlyTargetAndPreservesSource(t *testing.T) {
	f, service, request := migrationApplyFixture(t)
	source, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.Apply(request)
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || op.RehearsalBackupID != request.RehearsalBackupID {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	command, err := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(f.root, "rooms")).LoadCommand("r", "d", "power")
	if err != nil || command.Format != models.OperationFormatRaw || command.Count != 2 {
		t.Fatalf("command=%+v err=%v", command, err)
	}
	after, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json"))
	if err != nil || string(after) != string(source) {
		t.Fatalf("source changed: %v", err)
	}
	backup, err := f.backup.Load(op.ApplyBackupID)
	if err != nil || backup.Registry.Role != "apply" || backup.Registry.State != "point_in_time_verified" {
		t.Fatalf("backup=%+v err=%v", backup.Registry, err)
	}
	if _, err := service.Apply(request); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("repeat err=%v", err)
	}
}

func TestMigrationApplyRejectsStaleAndWrongRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, cleanupApplyFixture, *models.MigrationApplyRequest)
		want   error
	}{
		{"missing_plan", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			r.PlanID = "plan_00000000000000000000000000000000"
		}, repositories.ErrMaintenanceMissing},
		{"corrupt_plan", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			p := filepath.Join(filepath.Dir(f.root), "state", "plans", r.PlanID+".json")
			if err := os.WriteFile(p, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}, repositories.ErrMaintenanceCorrupt},
		{"wrong_backup", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			r.RehearsalBackupID = "backup_00000000000000000000000000000000"
		}, repositories.ErrBackupMissing},
		{"wrong_digest", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			r.PlanDigest = "sha256:" + strings.Repeat("0", 64)
		}, ErrMaintenancePlanStale},
		{"no_confirm", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) { r.Confirm = false }, ErrMigrationApplyInvalid},
		{"stale_source", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			if err := os.WriteFile(filepath.Join(f.root, "ir-signals/raw.json"), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		}, ErrMaintenancePlanStale},
		{"target_collision", func(t *testing.T, f cleanupApplyFixture, r *models.MigrationApplyRequest) {
			p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		}, ErrMigrationApplyStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, r := migrationApplyFixture(t)
			tc.change(t, f, &r)
			if _, err := s.Apply(r); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestMigrationApplyBackupFailureLeavesNoTarget(t *testing.T) {
	f, s, r := migrationApplyFixture(t)
	s.backup = migrationFailBackup{original: s.backup}
	op, err := s.Apply(r)
	if err == nil || op.ID == "" || op.Status != "partial_failure" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target exists: %v", err)
	}
}

func TestMigrationApplyCreatesStateAndExplicitScaffolds(t *testing.T) {
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	for _, rel := range []string{"rooms/r/room.json", "rooms/r/devices/d/device.json", "rooms/r/devices/d/adapters/ir/ir_config.json"} {
		if err := os.Remove(filepath.Join(f.root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	plans := NewMigrationPlanService(common)
	m := migrationDryRunRequest("raw").Mappings[0]
	m.TargetKind = "state"
	m.Decision.State = map[string]any{"mode": "cool"}
	m.Room = &models.Room{ID: "r", Name: "Room"}
	m.Device = &models.Device{ID: "d", Name: "Device", RoomID: "r", DeviceType: "ir", PrimaryAdapter: "ir"}
	m.IRConfig = &models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}
	m.StateSchema = models.IRStateSchema{"mode": {Type: models.IRStateFieldTypeEnum, Values: []string{"cool"}}}
	plan, err := NewMigrationDryRunService(f.root, plans).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{m}})
	if err != nil {
		t.Fatal(err)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	service := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root)
	op, err := service.Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err != nil || op.Status != "verified" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(f.root, "rooms"))
	state, err := repo.LoadState("r", "d", "power")
	if err != nil || state.State["mode"] != "cool" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	schema, err := repo.LoadStateSchema("r", "d")
	if err != nil || schema["mode"].Type != models.IRStateFieldTypeEnum {
		t.Fatalf("schema=%+v err=%v", schema, err)
	}
	verified, err := NewMigrationVerifyService(plans, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || verified.Status != "verified" || len(verified.Targets) != 1 || verified.Targets[0].Status != "verified" {
		t.Fatalf("state verify=%+v err=%v", verified, err)
	}
}

func TestMigrationApplyInterruptedCheckpointsRequireReview(t *testing.T) {
	for _, revision := range []uint64{2, 4, 5, 6, 7, 8, 9} {
		t.Run(strings.Repeat("r", int(revision)), func(t *testing.T) {
			f, s, r := migrationApplyFixture(t)
			s.operations = failingCleanupOperationStore{base: f.state.Operations(), failRevision: revision}
			op, err := s.Apply(r)
			if err == nil || op.ID == "" {
				t.Fatalf("op=%+v err=%v", op, err)
			}
			stored, err := f.state.Operations().Load(op.ID)
			if err != nil || stored.Status == "verified" {
				t.Fatalf("stored=%+v err=%v", stored, err)
			}
			if _, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json")); err != nil {
				t.Fatalf("source changed: %v", err)
			}
			if _, err := s.Apply(r); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
				t.Fatalf("retry err=%v", err)
			}
			reconciler := NewMaintenanceReconciler(f.state.Operations(), MaintenanceFilesystemInspector{RuntimeRoot: f.root})
			if err := reconciler.Scan(); err != nil {
				t.Fatal(err)
			}
			stored, err = f.state.Operations().Load(op.ID)
			if err != nil || stored.Status != "recovery_required" || stored.Recovery == nil || len(stored.Recovery.Actions) != 1 || stored.Recovery.Actions[0].Classification != MaintenanceAmbiguous {
				t.Fatalf("reconciled=%+v err=%v", stored, err)
			}
		})
	}
}

func TestMigrationApplyConcurrentSamePlanCreatesOnce(t *testing.T) {
	f, s, r := migrationApplyFixture(t)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Apply(r); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, repositories.ErrMaintenancePlanClaimed) && !errors.Is(err, ErrMigrationApplyStale) && !errors.Is(err, ErrMaintenancePlanStale) {
			t.Fatalf("err=%v", err)
		}
	}
	if success != 1 {
		t.Fatalf("success=%d", success)
	}
	if _, err := os.Stat(filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")); err != nil {
		t.Fatal(err)
	}
}

type migrationFailWriter struct{ calls int }

func (w *migrationFailWriter) WriteJSONNew(target string, value any, mode os.FileMode) error {
	w.calls++
	if w.calls == 1 {
		return errors.New("injected migration JSON write failure")
	}
	return repositories.NewAtomicJSONWriter().WriteJSONNew(target, value, mode)
}

type migrationFailNthWriter struct{ calls, failAt int }

func (w *migrationFailNthWriter) WriteJSONNew(target string, value any, mode os.FileMode) error {
	w.calls++
	if w.calls == w.failAt {
		return errors.New("injected second action write failure")
	}
	return repositories.NewAtomicJSONWriter().WriteJSONNew(target, value, mode)
}

func TestMigrationApplySecondActionFailurePreservesFirstAndSources(t *testing.T) {
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	writeMigrationFixture(t, f.root, "raw_median")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	plans := NewMigrationPlanService(common)
	first := migrationDryRunRequest("raw").Mappings[0]
	second := migrationDryRunRequest("raw_median").Mappings[0]
	second.ActionID = "migrate_second"
	second.TargetID = "second"
	second.Decision.TargetID = "second"
	second.Decision.ApproveRawMedian = true
	plan, err := NewMigrationDryRunService(f.root, plans).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{first.ActionID, second.ActionID}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	s := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root)
	s.writer.Writer = &migrationFailNthWriter{failAt: 2}
	op, err := s.Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err == nil || op.Status != "partial_failure" || op.ApplyBackupID == "" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	firstTarget := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
	secondTarget := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/second.json")
	if _, err := os.Stat(firstTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(secondTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second target exists: %v", err)
	}
	for _, source := range []string{"raw.json", "raw_median.json"} {
		if _, err := os.ReadFile(filepath.Join(f.root, "ir-signals", source)); err != nil {
			t.Fatalf("source %s: %v", source, err)
		}
	}
}

func TestMigrationApplyWriteFailureKeepsSourceAndOperation(t *testing.T) {
	f, s, r := migrationApplyFixture(t)
	s.writer.Writer = &migrationFailWriter{}
	op, err := s.Apply(r)
	if err == nil || op.ID == "" || op.Status != "partial_failure" || op.ApplyBackupID == "" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target exists: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(f.root, "ir-signals/raw.json")); err != nil {
		t.Fatalf("source changed: %v", err)
	}
	if _, err := s.Apply(r); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("retry err=%v", err)
	}
}

type migrationFailBackup struct{ original migrationApplyBackup }

func (f migrationFailBackup) Detail(id string) (models.BackupDetail, error) {
	return f.original.Detail(id)
}
func (migrationFailBackup) CreateApply(string) (models.BackupDetail, error) {
	return models.BackupDetail{}, errors.New("injected apply backup failure")
}
