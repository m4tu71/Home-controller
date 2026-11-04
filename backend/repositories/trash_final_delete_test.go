package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrashFinalDeleteRetentionAndClock(t *testing.T) {
	r, _, root, data := trashFixture(t)
	deleted := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return deleted }
	if _, err := r.Move(trashRequest()); err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 2) }
	preview, err := r.PreviewFinalDelete("entry_1")
	if err != nil || preview.Entry.EligibleAfter == nil || !preview.Entry.EligibleAfter.Equal(deleted.AddDate(0, 0, 30)) {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	shorter, err := NewTrashRepository(TrashConfig{Dir: root, RuntimeDataDir: data, StateDir: filepath.Join(filepath.Dir(root), "state"), RetentionDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	shorter.now = r.now
	if _, err := shorter.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil }); !errors.Is(err, ErrTrashFinalDeleteRetention) {
		t.Fatalf("shorter setting deleted early: %v", err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 1) }
	if _, err := r.PreviewFinalDelete("entry_1"); !errors.Is(err, ErrTrashClockRegression) {
		t.Fatalf("clock regression: %v", err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 31) }
	preview, err = r.PreviewFinalDelete("entry_1")
	if err != nil {
		t.Fatal(err)
	}
	otherSource := filepath.Join(data, "rooms", "other.json")
	if err := os.WriteFile(otherSource, []byte(`{"other":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	otherRequest := trashRequest()
	otherRequest.EntryID, otherRequest.SourceRelativePath, otherRequest.OriginalID = "entry_2", "rooms/other.json", "other"
	if _, err := r.Move(otherRequest); err != nil {
		t.Fatal(err)
	}
	result, err := r.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil })
	if err != nil || result.Status != "final_deleted" || result.DeletedFiles != 1 {
		t.Fatalf("delete result: %+v %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "entry_1", "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("payload remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "entry_2", "payload")); err != nil {
		t.Fatalf("additional entry changed: %v", err)
	}
	entry, err := r.Load("entry_1")
	if err != nil || entry.FinalDeletedAt == nil || entry.FinalDeletePlanID != "plan_1" || entry.RestoreAvailable {
		t.Fatalf("tombstone: %+v %v", entry, err)
	}
	if _, err := r.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil }); !errors.Is(err, ErrTrashFinalDeleteState) {
		t.Fatalf("repository repeated delete: %v", err)
	}
}

func TestTrashFinalDeleteInterruptedDirectoryResumesOnlySavedPaths(t *testing.T) {
	r, _, root, data := trashFixture(t)
	dir := filepath.Join(data, "rooms", "legacy")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"value":1}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	deleted := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return deleted }
	request := trashRequest()
	request.EntryID, request.SourceRelativePath, request.OriginalID = "legacy_1", "rooms/legacy", "legacy"
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 31) }
	preview, err := r.PreviewFinalDelete("legacy_1")
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected file interruption")
	result, err := r.DeleteFinal(preview, "plan_1", "op_final", func(deleted int64) error {
		if deleted == 1 {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) || result.DeletedFiles != 1 || result.Status != "final_deleting" {
		t.Fatalf("interrupted: %+v %v", result, err)
	}
	entry, err := r.Load("legacy_1")
	if err != nil || entry.FinalDeleteFilesDeleted != 1 || entry.FinalDeletePlanID != "plan_1" {
		t.Fatalf("progress: %+v %v", entry, err)
	}
	remaining := filepath.Join(root, "legacy_1", "payload", "b.json")
	if err := os.WriteFile(remaining, []byte(`{"value":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil }); !errors.Is(err, ErrTrashChanged) {
		t.Fatalf("changed remaining file accepted: %v", err)
	}
	if err := os.WriteFile(remaining, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy_1", "payload", "added.json"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil }); !errors.Is(err, ErrTrashChanged) {
		t.Fatalf("added file accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "legacy_1", "payload", "added.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteFinal(preview, "other_plan", "op_final", func(int64) error { return nil }); !errors.Is(err, ErrTrashFinalDeleteState) {
		t.Fatalf("different plan resumed: %v", err)
	}
	result, err = r.DeleteFinal(preview, "plan_1", "op_final", func(int64) error { return nil })
	if err != nil || result.DeletedFiles != 2 || result.Status != "final_deleted" {
		t.Fatalf("resume: %+v %v", result, err)
	}
}

func TestTrashFinalDeleteRejectsSymlinkAndInvalidRetentionEnv(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc"} {
		t.Run("retention_"+value, func(t *testing.T) {
			t.Setenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS", value)
			if _, err := NewTrashRepositoryFromEnv(); !errors.Is(err, ErrTrashBoundary) {
				t.Fatalf("retention=%q error=%v", value, err)
			}
		})
	}
	r, source, root, _ := trashFixture(t)
	deleted := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return deleted }
	if _, err := r.Move(trashRequest()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "entry_1", "payload")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(root, "entry_1", "payload")); err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 31) }
	if _, err := r.PreviewFinalDelete("entry_1"); err == nil {
		t.Fatal("symlink payload accepted")
	}
}

func TestTrashFinalDeleteEmptyDirectory(t *testing.T) {
	r, _, root, data := trashFixture(t)
	empty := filepath.Join(data, "rooms", "empty")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	deleted := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return deleted }
	request := trashRequest()
	request.EntryID, request.SourceRelativePath, request.OriginalID = "empty_1", "rooms/empty", "empty"
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return deleted.AddDate(0, 0, 31) }
	snapshot, err := r.PreviewFinalDelete("empty_1")
	if err != nil || len(snapshot.Files) != 0 || len(snapshot.Directories) != 1 {
		t.Fatalf("empty directory snapshot: %+v %v", snapshot, err)
	}
	result, err := r.DeleteFinal(snapshot, "plan_empty", "op_empty", func(int64) error { return nil })
	if err != nil || result.Status != "final_deleted" || result.TotalFiles != 0 {
		t.Fatalf("empty directory delete: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "empty_1", "metadata.json")); err != nil {
		t.Fatalf("metadata tombstone missing: %v", err)
	}
}
