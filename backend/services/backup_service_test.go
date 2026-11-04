package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
)

type backupOperationFixture struct {
	value    models.MaintenanceOperation
	claimErr error
}

func (f *backupOperationFixture) Load(string) (models.MaintenanceOperation, error) {
	return f.value, nil
}
func (f *backupOperationFixture) Update(v models.MaintenanceOperation) error { f.value = v; return nil }
func (f *backupOperationFixture) HasExclusivePlanClaim(string, string) error { return f.claimErr }

func TestBackupServiceApplyAssociation(t *testing.T) {
	base := t.TempDir()
	config := repositories.BackupConfig{Dir: filepath.Join(base, "backups"), RuntimeDataDir: filepath.Join(base, "data"), StateDir: filepath.Join(base, "state"), TrashDir: filepath.Join(base, "trash"), RepositoryDir: filepath.Join(base, "repo"), WebRootDir: filepath.Join(base, "web")}
	for _, dir := range []string{config.Dir, config.RuntimeDataDir, config.StateDir, filepath.Join(config.StateDir, "plans"), filepath.Join(config.StateDir, "operations"), config.TrashDir, config.RepositoryDir, config.WebRootDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config.RuntimeDataDir, "sample.bin"), []byte("fixture data"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := repositories.NewBackupRepository(config)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: config.StateDir, RuntimeDataDir: config.RuntimeDataDir, RepositoryDir: config.RepositoryDir, WebRootDir: config.WebRootDir, BackupDir: config.Dir, TrashDir: config.TrashDir})
	if err != nil {
		t.Fatal(err)
	}
	plans := NewMaintenancePlanService(state.Plans(), repo)
	plan, err := plans.Create(models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: "migration.v1", Payload: []byte(`{"source":"rooms/room_1"}`), ReadyToApply: true, BackupRequirement: "rehearsal_and_apply", Actions: []models.MaintenancePlanAction{{ID: "action_1", Action: "copy", Selectable: true, Destructive: true, BackupSources: []string{"sample.bin"}}}})
	if err != nil {
		t.Fatal(err)
	}
	op := &backupOperationFixture{value: models.MaintenanceOperation{Version: 1, ID: "op_1", Revision: 2, StartedAt: time.Now(), Target: "migration", PlanID: plan.PlanID, PlanDigest: plan.Digest, Type: "migration", Actions: []models.MaintenanceOperationAction{{ID: "action_1", Action: "copy", Status: "pending"}}, Status: "applying"}}
	s := NewBackupService(repo, op).WithPlanService(plans)
	rehearsal, err := s.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{"action_1"}})
	if err != nil || rehearsal.Registry.State != "snapshot_verified" {
		t.Fatalf("rehearsal=%+v err=%v", rehearsal, err)
	}
	if _, err := repo.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	op.value.RehearsalBackupID = rehearsal.Bundle.ID
	apply, err := s.CreateApply("op_1")
	if err != nil || apply.Registry.State != "point_in_time_verified" || apply.Bundle.ID == rehearsal.Bundle.ID || op.value.ApplyBackupID != apply.Bundle.ID || op.value.Revision != 3 {
		t.Fatalf("apply=%+v op=%+v err=%v", apply, op.value, err)
	}
	if _, err := s.CreateApply("op_1"); err == nil {
		t.Fatal("double apply backup allowed")
	}
}
