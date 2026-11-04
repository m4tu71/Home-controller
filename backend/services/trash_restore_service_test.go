package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"backend/models"
	"backend/repositories"
)

type restoreServiceFixture struct {
	preview         repositories.TrashRestorePreview
	payload, target string
	fail            bool
}

func (f *restoreServiceFixture) PreviewRestore(string) (repositories.TrashRestorePreview, error) {
	return f.preview, nil
}
func (f *restoreServiceFixture) Restore(_, _, _ string) (models.TrashEntry, error) {
	if f.fail {
		return models.TrashEntry{}, errors.New("injected rename interruption")
	}
	if err := os.Rename(f.payload, f.target); err != nil {
		return models.TrashEntry{}, err
	}
	return models.TrashEntry{ID: "entry_1", Status: "restored"}, nil
}

func TestTrashRestoreServiceOperationRecord(t *testing.T) {
	for _, fail := range []bool{false, true} {
		base := t.TempDir()
		runtime := filepath.Join(base, "data")
		trash := filepath.Join(base, "trash")
		for _, dir := range []string{runtime, filepath.Join(runtime, "rooms"), trash, filepath.Join(trash, "entry_1")} {
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		payload := filepath.Join(trash, "entry_1", "payload")
		target := filepath.Join(runtime, "rooms", "room_1")
		if err := os.WriteFile(payload, []byte("room"), 0600); err != nil {
			t.Fatal(err)
		}
		fixture := &restoreServiceFixture{preview: repositories.TrashRestorePreview{EntryID: "entry_1", OriginalRelativePath: "rooms/room_1", ExpectedInventoryHash: "sha256:abc", RestoreAvailable: true}, payload: payload, target: target, fail: fail}
		operations := &memoryMaintenanceStore{}
		s := NewTrashRestoreService(fixture, operations, runtime, trash)
		_, id, err := s.Restore("entry_1", "sha256:abc")
		if id == "" {
			t.Fatal("operation ID missing")
		}
		if fail {
			if err == nil || operations.record.Status != "partial_failure" {
				t.Fatalf("error=%v status=%s", err, operations.record.Status)
			}
			if _, err := os.Stat(payload); err != nil {
				t.Fatal(err)
			}
		} else if err != nil || operations.record.Status != "verified" || operations.record.Result != "verified" {
			t.Fatalf("error=%v record=%+v", err, operations.record)
		}
		if operations.record.Type != "trash_restore" || operations.record.ID != id || len(operations.record.Events) == 0 {
			t.Fatalf("record=%+v", operations.record)
		}
	}
}
