package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"backend/models"
	"backend/repositories"
)

type cleanupApplyFixture struct {
	root, trashRoot string
	backup          *repositories.BackupRepository
	trash           *repositories.TrashRepository
	state           *repositories.MaintenanceState
	plan            *CleanupPlanService
	service         *CleanupApplyService
}

func newCleanupApplyFixture(t *testing.T) cleanupApplyFixture {
	t.Helper()
	base := t.TempDir()
	c := repositories.BackupConfig{Dir: filepath.Join(base, "backup"), RuntimeDataDir: filepath.Join(base, "data"), RepositoryDir: filepath.Join(base, "repo"), WebRootDir: filepath.Join(base, "web"), StateDir: filepath.Join(base, "state"), TrashDir: filepath.Join(base, "trash")}
	for _, p := range []string{c.Dir, c.RuntimeDataDir, c.RepositoryDir, c.WebRootDir, c.StateDir, filepath.Join(c.StateDir, "plans"), filepath.Join(c.StateDir, "operations"), c.TrashDir} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := repositories.NewBackupRepository(c)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: c.StateDir, RuntimeDataDir: c.RuntimeDataDir, RepositoryDir: c.RepositoryDir, WebRootDir: c.WebRootDir, BackupDir: c.Dir, TrashDir: c.TrashDir})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{Dir: c.TrashDir, RuntimeDataDir: c.RuntimeDataDir, RepositoryDir: c.RepositoryDir, WebRootDir: c.WebRootDir, StateDir: c.StateDir, BackupDir: c.Dir, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	plans := NewMaintenancePlanService(state.Plans(), backup)
	cleanup := NewCleanupPlanService(NewDataHealthService(repositories.NewDataHealthRepository(c.RuntimeDataDir)), plans)
	apply := NewCleanupApplyService(cleanup, state.Operations(), NewBackupService(backup, state.Operations()).WithPlanService(plans), trash, c.RuntimeDataDir, c.TrashDir)
	var index atomic.Int64
	apply.newID = func(prefix string) (string, error) {
		return prefix + strings.Repeat("a", 30) + string(rune('a'+index.Add(1))), nil
	}
	return cleanupApplyFixture{root: c.RuntimeDataDir, trashRoot: c.TrashDir, backup: backup, trash: trash, state: state, plan: cleanup, service: apply}
}

func (f cleanupApplyFixture) put(t *testing.T, rel, data string) {
	t.Helper()
	p := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f cleanupApplyFixture) request(t *testing.T, ids ...string) models.CleanupApplyRequest {
	t.Helper()
	selected := make([]models.CleanupIssueSelection, 0, len(ids))
	for _, id := range ids {
		selected = append(selected, selectedIssue(t, f.plan, id))
	}
	plan, err := f.plan.Create(selected)
	if err != nil {
		t.Fatal(err)
	}
	return models.CleanupApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, Issues: selected, ActionIDs: func() []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, cleanupActionID(id))
		}
		return out
	}(), Confirm: true}
}

func TestCleanupApplyInvalidAndEmptyJSONBackupThenTrash(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/invalid.json", "{")
	f.put(t, "ir-signals/empty.json", "")
	f.put(t, "ir-signals/untouched.json", `{"format":"raw_samples"}`)
	req := f.request(t, "invalid_json:ir-signals/invalid.json", "empty_json:ir-signals/empty.json")
	op, err := f.service.Apply(req)
	if err != nil || op.Status != "verified" || op.ApplyBackupID == "" || len(op.Actions) != 2 {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	loaded, err := f.state.Operations().Load(op.ID)
	if err != nil || loaded.ApplyBackupID != op.ApplyBackupID || loaded.Status != "verified" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	bundle, err := f.backup.Load(op.ApplyBackupID)
	if err != nil || bundle.Registry.Role != "apply" || bundle.Registry.State != "point_in_time_verified" {
		t.Fatalf("backup=%+v err=%v", bundle.Registry, err)
	}
	for _, tc := range []struct{ rel, data string }{{"ir-signals/invalid.json", "{"}, {"ir-signals/empty.json", ""}} {
		if _, err := os.Lstat(filepath.Join(f.root, tc.rel)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source remained %s: %v", tc.rel, err)
		}
		saved, err := os.ReadFile(filepath.Join(filepath.Dir(f.root), "backup", op.ApplyBackupID, "data", tc.rel))
		if err != nil || string(saved) != tc.data {
			t.Fatalf("backup bytes %s=%q err=%v", tc.rel, saved, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(f.root, "ir-signals", "untouched.json")); err != nil || !strings.Contains(string(data), "raw_samples") {
		t.Fatalf("unselected changed: %q %v", data, err)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	for _, entry := range entries {
		if entry.Status != "sealed" || entry.OperationID != op.ID || !entry.RestoreAvailable {
			t.Fatalf("entry=%+v", entry)
		}
	}
	if _, err := f.service.Apply(req); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("duplicate err=%v", err)
	}
}

func TestCleanupApplyRejectsInvalidPlanAndStaleBeforeMutation(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/bad.json", "{")
	req := f.request(t, "invalid_json:ir-signals/bad.json")
	for _, tc := range []struct {
		name   string
		change func(*models.CleanupApplyRequest)
		want   error
	}{
		{"missing plan", func(r *models.CleanupApplyRequest) { r.PlanID = "plan_00000000000000000000000000000000" }, repositories.ErrMaintenanceMissing},
		{"wrong digest", func(r *models.CleanupApplyRequest) { r.PlanDigest = "sha256:" + strings.Repeat("0", 64) }, ErrMaintenancePlanStale},
		{"unknown action", func(r *models.CleanupApplyRequest) { r.ActionIDs = []string{"missing"} }, ErrMaintenancePlanActionMissing},
		{"wrong issue", func(r *models.CleanupApplyRequest) { r.Issues[0].IssueID = "invalid_json:ir-signals/other.json" }, ErrCleanupSelectionInvalid},
		{"confirm false", func(r *models.CleanupApplyRequest) { r.Confirm = false }, ErrCleanupSelectionInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyReq := req
			copyReq.Issues = append([]models.CleanupIssueSelection(nil), req.Issues...)
			tc.change(&copyReq)
			if _, err := f.service.Apply(copyReq); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
	f.put(t, "ir-signals/bad.json", "{{")
	if _, err := f.service.Apply(req); !errors.Is(err, ErrMaintenancePlanStale) {
		t.Fatalf("stale target err=%v", err)
	}
	if ops, err := f.state.Operations().List(); err != nil || len(ops) != 0 {
		t.Fatalf("unexpected operations=%+v err=%v", ops, err)
	}
}

type failingCleanupBackup struct{ err error }

func (f failingCleanupBackup) CreateApply(string) (models.BackupDetail, error) {
	return models.BackupDetail{}, f.err
}

type failingCleanupTrash struct {
	base          *repositories.TrashRepository
	failAt, calls int
	err           error
}

func (f *failingCleanupTrash) Move(r repositories.TrashMoveRequest) (repositories.TrashMoveResult, error) {
	f.calls++
	if f.calls == f.failAt {
		return repositories.TrashMoveResult{}, f.err
	}
	return f.base.Move(r)
}
func (f *failingCleanupTrash) Load(id string) (models.TrashEntry, error) { return f.base.Load(id) }

func TestCleanupApplyBackupFailureStopsBeforeMutation(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/bad.json", "{")
	req := f.request(t, "invalid_json:ir-signals/bad.json")
	f.service.backup = failingCleanupBackup{err: errors.New("backup unavailable")}
	op, err := f.service.Apply(req)
	if err == nil || op.Status != "partial_failure" || op.ApplyBackupID != "" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	if data, err := os.ReadFile(filepath.Join(f.root, "ir-signals", "bad.json")); err != nil || string(data) != "{" {
		t.Fatalf("runtime changed: %q %v", data, err)
	}
	if entries, err := f.trash.List(); err != nil || len(entries) != 0 {
		t.Fatalf("trash=%+v err=%v", entries, err)
	}
}

func TestCleanupApplyPartialFailureDoesNotMutateUnselectedOrRetry(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/one.json", "{")
	f.put(t, "ir-signals/two.json", "")
	f.put(t, "ir-signals/unselected.json", `{"format":"raw_samples"}`)
	req := f.request(t, "invalid_json:ir-signals/one.json", "empty_json:ir-signals/two.json")
	failed := &failingCleanupTrash{base: f.trash, failAt: 2, err: repositories.ErrTrashCrossDevice}
	f.service.trash = failed
	op, err := f.service.Apply(req)
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || !errors.Is(err, repositories.ErrTrashCrossDevice) || op.Status != "partial_failure" || op.ApplyBackupID == "" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "ir-signals", "unselected.json")); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, rel := range []string{"ir-signals/one.json", "ir-signals/two.json"} {
		if _, err := os.Stat(filepath.Join(f.root, rel)); err == nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("remaining selected sources=%d", count)
	}
	if _, err := f.service.Apply(req); !errors.Is(err, repositories.ErrMaintenancePlanClaimed) {
		t.Fatalf("retry err=%v", err)
	}
}

func TestCleanupApplyRejectsSymlinkAndChangedTargetBeforeOperation(t *testing.T) {
	for _, tc := range []string{"symlink", "changed bytes"} {
		t.Run(tc, func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			f.put(t, "ir-signals/bad.json", "{")
			req := f.request(t, "invalid_json:ir-signals/bad.json")
			target := filepath.Join(f.root, "ir-signals", "bad.json")
			if tc == "symlink" {
				outside := filepath.Join(t.TempDir(), "outside.json")
				if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			} else {
				f.put(t, "ir-signals/bad.json", "{{")
			}
			if _, err := f.service.Apply(req); err == nil {
				t.Fatal("changed target accepted")
			}
			ops, err := f.state.Operations().List()
			if err != nil || len(ops) != 0 {
				t.Fatalf("operations=%+v err=%v", ops, err)
			}
		})
	}
}

func TestCleanupApplyRenameFailureRecordsPartialFailure(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/bad.json", "{")
	req := f.request(t, "invalid_json:ir-signals/bad.json")
	renamed := &failingCleanupTrash{base: f.trash, failAt: 1, err: &repositories.TrashMoveError{Stage: repositories.TrashStageRename, Err: errors.New("rename unavailable")}}
	f.service.trash = renamed
	op, err := f.service.Apply(req)
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || op.Status != "partial_failure" || op.ApplyBackupID == "" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	if data, err := os.ReadFile(filepath.Join(f.root, "ir-signals", "bad.json")); err != nil || string(data) != "{" {
		t.Fatalf("source changed: %q %v", data, err)
	}
}

type failingCleanupOperationStore struct {
	base         *repositories.MaintenanceOperationRepository
	failRevision uint64
}

func (f failingCleanupOperationStore) Create(op models.MaintenanceOperation) error {
	return f.base.Create(op)
}
func (f failingCleanupOperationStore) Update(op models.MaintenanceOperation) error {
	if op.Revision == f.failRevision {
		return errors.New("simulated state interruption")
	}
	return f.base.Update(op)
}
func (f failingCleanupOperationStore) Load(id string) (models.MaintenanceOperation, error) {
	return f.base.Load(id)
}
func (f failingCleanupOperationStore) List() ([]models.MaintenanceOperation, error) {
	return f.base.List()
}

func TestCleanupApplyInterruptedCheckpointsDoNotAutoRetry(t *testing.T) {
	for _, revision := range []uint64{2, 4, 5, 6, 7, 8, 9} {
		t.Run(strings.Repeat("r", int(revision)), func(t *testing.T) {
			f := newCleanupApplyFixture(t)
			f.put(t, "ir-signals/bad.json", "{")
			req := f.request(t, "invalid_json:ir-signals/bad.json")
			f.service.operations = failingCleanupOperationStore{base: f.state.Operations(), failRevision: revision}
			op, err := f.service.Apply(req)
			if err == nil || op.ID == "" {
				t.Fatalf("op=%+v err=%v", op, err)
			}
			stored, loadErr := f.state.Operations().Load(op.ID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if stored.Status == "verified" {
				t.Fatalf("interrupted operation verified: %+v", stored)
			}
			_, sourceErr := os.Stat(filepath.Join(f.root, "ir-signals", "bad.json"))
			if revision < 5 && sourceErr != nil {
				t.Fatalf("source moved before checkpoint %d: %v", revision, sourceErr)
			}
			if revision == 5 && !errors.Is(sourceErr, os.ErrNotExist) {
				t.Fatalf("source not moved at post-rename checkpoint: %v", sourceErr)
			}
			if _, retryErr := f.service.Apply(req); !errors.Is(retryErr, repositories.ErrMaintenancePlanClaimed) {
				t.Fatalf("auto retry allowed: %v", retryErr)
			}
		})
	}
}

func TestCleanupApplyConcurrentSamePlanMovesOnce(t *testing.T) {
	f := newCleanupApplyFixture(t)
	f.put(t, "ir-signals/bad.json", "{")
	req := f.request(t, "invalid_json:ir-signals/bad.json")
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.service.Apply(req); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, repositories.ErrMaintenancePlanClaimed) && !errors.Is(err, ErrMaintenancePlanStale) && !errors.Is(err, ErrCleanupIssueStale) {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("successful applies=%d", success)
	}
	entries, err := f.trash.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
}
