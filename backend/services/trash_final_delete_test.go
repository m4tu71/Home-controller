package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
)

type finalDeleteFixture struct {
	service *TrashFinalDeleteService
	trash   *repositories.TrashRepository
	state   *repositories.MaintenanceState
	root    string
	entryID string
}

func newFinalDeleteFixture(t *testing.T, operationType string, withBackup bool) finalDeleteFixture {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	trashRoot := filepath.Join(base, "trash")
	stateRoot := filepath.Join(base, "state")
	backupRoot := filepath.Join(base, "backup")
	repoRoot := filepath.Join(base, "repo")
	webRoot := filepath.Join(base, "web")
	for _, dir := range []string{data, filepath.Join(data, "legacy"), trashRoot, stateRoot, filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), backupRoot, repoRoot, webRoot} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	sourceRel := "legacy/item.json"
	if err := os.WriteFile(filepath.Join(data, sourceRel), []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{Dir: trashRoot, RuntimeDataDir: data, RepositoryDir: repoRoot, WebRootDir: webRoot, StateDir: stateRoot, BackupDir: backupRoot, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: stateRoot, RuntimeDataDir: data, RepositoryDir: repoRoot, WebRootDir: webRoot, BackupDir: backupRoot, TrashDir: trashRoot})
	if err != nil {
		t.Fatal(err)
	}
	backups, err := repositories.NewBackupRepository(repositories.BackupConfig{Dir: backupRoot, RuntimeDataDir: data, RepositoryDir: repoRoot, WebRootDir: webRoot, StateDir: stateRoot, TrashDir: trashRoot})
	if err != nil {
		t.Fatal(err)
	}
	op := models.MaintenanceOperation{Version: 1, ID: "original_op", Revision: 1, StartedAt: time.Now().UTC().Add(-40 * 24 * time.Hour), Target: sourceRel, Type: operationType, Status: "verified"}
	if withBackup {
		op.PlanID = "plan_original"
		op.PlanDigest = "sha256:" + strings.Repeat("a", 64)
		backup, err := backups.Create(repositories.BackupCreate{ID: "backup_original", Role: "apply", PlanID: op.PlanID, ActionIDs: []string{"entry"}, Sources: []string{sourceRel}})
		if err != nil || backup.Registry.State != "point_in_time_verified" {
			t.Fatalf("backup: %+v %v", backup, err)
		}
		op.ApplyBackupID = backup.Bundle.ID
	}
	if err := state.Operations().Create(op); err != nil {
		t.Fatal(err)
	}
	if _, err := trash.Move(repositories.TrashMoveRequest{EntryID: "entry_1", SourceRelativePath: sourceRel, OriginalID: "item", TargetType: "legacy_ir_signal", Reason: "operator delete", OperationType: operationType, OperationID: op.ID}); err != nil {
		t.Fatal(err)
	}
	entry, err := trash.Load("entry_1")
	if err != nil {
		t.Fatal(err)
	}
	deleted := time.Now().UTC().AddDate(0, 0, -40)
	eligible := deleted.AddDate(0, 0, 30)
	entry.DeletedAt, entry.EligibleAfter = &deleted, &eligible
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashRoot, "entry_1", "metadata.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	plans := NewMaintenancePlanService(state.Plans(), nil)
	service := NewTrashFinalDeleteService(trash, plans, state.Operations(), backups, trashRoot)
	return finalDeleteFixture{service: service, trash: trash, state: state, root: trashRoot, entryID: "entry_1"}
}

func TestTrashFinalDeleteServiceNormalAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		opType     string
		withBackup bool
	}{{"normal_delete", "delete", false}, {"legacy_cleanup", "cleanup", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFinalDeleteFixture(t, tc.opType, tc.withBackup)
			preview, err := f.service.DryRun(f.entryID)
			if err != nil || !preview.Eligible || preview.PlanID == "" || preview.PlanDigest == "" || preview.Inventory.FileCount != 1 {
				t.Fatalf("preview: %+v %v", preview, err)
			}
			if _, err := f.service.Apply(preview.PlanID, "wrong_entry"); !errors.Is(err, ErrTrashFinalDeleteConfirmation) {
				t.Fatalf("wrong confirmation: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.root, f.entryID, "payload")); err != nil {
				t.Fatalf("payload changed before confirmation: %v", err)
			}
			result, err := f.service.Apply(preview.PlanID, f.entryID)
			if err != nil || result.Status != "final_deleted" || result.DeletedFiles != 1 || result.OperationID == "" {
				t.Fatalf("apply: %+v %v", result, err)
			}
			operation, err := f.state.Operations().Load(result.OperationID)
			if err != nil || operation.Status != "verified" || operation.PlanID != preview.PlanID || operation.Actions[0].Status != "verified" {
				t.Fatalf("operation: %+v %v", operation, err)
			}
			again, err := f.service.Apply(preview.PlanID, f.entryID)
			if err != nil || again.OperationID != result.OperationID || again.DeletedFiles != result.DeletedFiles {
				t.Fatalf("repeat apply: %+v %v", again, err)
			}
		})
	}
}

func TestTrashFinalDeleteRejectsBrokenBackupAssociation(t *testing.T) {
	f := newFinalDeleteFixture(t, "cleanup", true)
	op, err := f.state.Operations().Load("original_op")
	if err != nil {
		t.Fatal(err)
	}
	op.ApplyBackupID = "missing_backup"
	op.Revision++
	if err := f.state.Operations().Update(op); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DryRun(f.entryID); !errors.Is(err, ErrTrashFinalDeleteAssociation) {
		t.Fatalf("broken backup relation: %v", err)
	}
}

func TestTrashFinalDeleteIneligiblePreviewAndMissingOperation(t *testing.T) {
	f := newFinalDeleteFixture(t, "delete", false)
	entry, err := f.trash.Load(f.entryID)
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(24 * time.Hour)
	entry.EligibleAfter = &future
	encoded, _ := json.Marshal(entry)
	metadata := filepath.Join(f.root, f.entryID, "metadata.json")
	if err := os.WriteFile(metadata, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := f.service.DryRun(f.entryID)
	if err != nil || preview.Eligible || preview.PlanID != "" || preview.CurrentTime.IsZero() || !preview.EligibleAfter.Equal(future) {
		t.Fatalf("ineligible preview: %+v %v", preview, err)
	}
	entry, err = f.trash.Load(f.entryID)
	if err != nil || entry.LastCheckedAt == nil {
		t.Fatalf("last check not recorded: %+v %v", entry, err)
	}
	past := time.Now().UTC().Add(-24 * time.Hour)
	entry.EligibleAfter = &past
	entry.OperationID = "missing_op"
	encoded, _ = json.Marshal(entry)
	if err := os.WriteFile(metadata, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DryRun(f.entryID); !errors.Is(err, ErrTrashFinalDeleteAssociation) {
		t.Fatalf("missing original operation: %v", err)
	}
}

type interruptingFinalDeleteStore struct {
	trash *repositories.TrashRepository
	fail  bool
}

func (s *interruptingFinalDeleteStore) PreviewFinalDelete(id string) (repositories.TrashFinalDeleteSnapshot, error) {
	return s.trash.PreviewFinalDelete(id)
}
func (s *interruptingFinalDeleteStore) Load(id string) (models.TrashEntry, error) {
	return s.trash.Load(id)
}
func (s *interruptingFinalDeleteStore) DeleteFinal(snapshot repositories.TrashFinalDeleteSnapshot, planID, opID string, progress func(int64) error) (repositories.TrashFinalDeleteResult, error) {
	return s.trash.DeleteFinal(snapshot, planID, opID, func(deleted int64) error {
		if err := progress(deleted); err != nil {
			return err
		}
		if s.fail && deleted == 1 {
			return errors.New("injected file interruption")
		}
		return nil
	})
}

func TestTrashFinalDeleteServiceInterruptedAndResumed(t *testing.T) {
	f := newFinalDeleteFixture(t, "delete", false)
	f.service.trash = &interruptingFinalDeleteStore{trash: f.trash, fail: true}
	preview, err := f.service.DryRun(f.entryID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Apply(preview.PlanID, f.entryID)
	if err == nil || result.DeletedFiles != 1 || result.Status != "final_deleting" {
		t.Fatalf("interrupted: %+v %v", result, err)
	}
	op, err := f.state.Operations().Load(result.OperationID)
	if err != nil || op.Status != "partial_failure" {
		t.Fatalf("interrupted operation: %+v %v", op, err)
	}
	f.service.trash = f.trash
	result, err = f.service.Apply(preview.PlanID, f.entryID)
	if err != nil || result.Status != "final_deleted" || result.OperationID != op.ID {
		t.Fatalf("resumed: %+v %v", result, err)
	}
}

func TestTrashFinalDeletePlanPayloadHasStableInventory(t *testing.T) {
	f := newFinalDeleteFixture(t, "delete", false)
	preview, err := f.service.DryRun(f.entryID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.plans.Load(preview.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	var payload TrashFinalDeletePlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Snapshot.Files) != 1 || payload.Snapshot.Files[0].SHA256 != hashFinalDeleteFixture([]byte(`{"value":1}`)) {
		t.Fatalf("snapshot: %+v", payload.Snapshot)
	}
}

func hashFinalDeleteFixture(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
