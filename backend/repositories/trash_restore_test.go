package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sealedRoomFixture(t *testing.T) (*TrashRepository, string, string, string) {
	t.Helper()
	r, _, trash, data := trashFixture(t)
	source := filepath.Join(data, "rooms", "room_1")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "room.json"), []byte(`{"id":"room_1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	request := trashRequest()
	request.SourceRelativePath = "rooms/room_1"
	request.OriginalID = "room_1"
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	return r, source, trash, data
}

func sealedJSONFixture(t *testing.T, targetType string) (*TrashRepository, string) {
	t.Helper()
	r, _, _, data := trashFixture(t)
	var relative, originalID string
	switch targetType {
	case "room":
		relative, originalID = "rooms/room_1/room.json", "room_1"
	case "device":
		relative, originalID = "rooms/room_1/devices/device_1/device.json", "device_1"
	default:
		t.Fatal("invalid fixture type")
	}
	source := filepath.Join(data, relative)
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"id":"`+originalID+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	request := trashRequest()
	request.SourceRelativePath, request.OriginalID, request.TargetType = relative, originalID, targetType
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	return r, source
}

func TestTrashRestoreJSONOnlyRoomAndDevice(t *testing.T) {
	for _, targetType := range []string{"room", "device"} {
		t.Run(targetType, func(t *testing.T) {
			r, source := sealedJSONFixture(t, targetType)
			if info, err := os.Stat(filepath.Dir(source)); err != nil || !info.IsDir() {
				t.Fatalf("parent directory missing: %v", err)
			}
			preview, err := r.PreviewRestore("entry_1")
			if err != nil || !preview.PathAvailable || !preview.IDAvailable || !preview.RestoreAvailable {
				t.Fatalf("preview=%+v err=%v", preview, err)
			}
			if _, err := r.Restore("entry_1", preview.ExpectedInventoryHash, "restore_1"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatalf("JSON not restored: %v", err)
			}
			// A second fixture with the same retained directory detects a real JSON path collision.
			r2, source2 := sealedJSONFixture(t, targetType)
			if err := os.WriteFile(source2, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			collision, err := r2.PreviewRestore("entry_1")
			if err != nil || collision.PathAvailable || collision.RestoreAvailable || collision.Reason != "path_collision" {
				t.Fatalf("collision=%+v err=%v", collision, err)
			}
			if _, err := r2.Restore("entry_1", collision.ExpectedInventoryHash, "restore_2"); !errors.Is(err, ErrTrashChanged) {
				t.Fatalf("collision restore: %v", err)
			}
			r3, source3 := sealedJSONFixture(t, targetType)
			id := "room_1"
			if targetType == "device" {
				id = "device_1"
			}
			alternate := filepath.Join(filepath.Dir(filepath.Dir(source3)), id+".json")
			if err := os.WriteFile(alternate, []byte("other ID occupant"), 0600); err != nil {
				t.Fatal(err)
			}
			idCollision, err := r3.PreviewRestore("entry_1")
			if err != nil || !idCollision.PathAvailable || idCollision.IDAvailable || idCollision.RestoreAvailable || idCollision.Reason != "id_collision" {
				t.Fatalf("ID collision=%+v err=%v", idCollision, err)
			}
		})
	}
}

func TestTrashRestoreSuccessAndDoubleRestore(t *testing.T) {
	r, source, _, _ := sealedRoomFixture(t)
	p, err := r.PreviewRestore("entry_1")
	if err != nil || !p.RestoreAvailable || !p.PathAvailable || !p.IDAvailable || p.ExpectedInventoryHash == "" {
		t.Fatalf("preview=%+v err=%v", p, err)
	}
	entry, err := r.Restore("entry_1", p.ExpectedInventoryHash, "restore_1")
	if err != nil || entry.Status != "restored" || entry.RestoreOperationID != "restore_1" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	if _, err := os.Stat(filepath.Join(source, "room.json")); err != nil {
		t.Fatal(err)
	}
	if classification, err := r.Reconcile("entry_1"); err != nil || classification != "restored_unconfirmed" {
		t.Fatalf("reconcile=%s %v", classification, err)
	}
	if _, err := r.Restore("entry_1", p.ExpectedInventoryHash, "restore_2"); !errors.Is(err, ErrTrashChanged) {
		t.Fatalf("double restore: %v", err)
	}
}

func TestTrashRestoreConflicts(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{{"path", "path"}, {"id", "id"}, {"hash", "hash"}, {"symlink", "symlink"}, {"payload", "payload"}} {
		t.Run(tc.name, func(t *testing.T) {
			r, source, trash, data := sealedRoomFixture(t)
			p, err := r.PreviewRestore("entry_1")
			if err != nil {
				t.Fatal(err)
			}
			switch tc.kind {
			case "path":
				if err := os.Mkdir(source, 0700); err != nil {
					t.Fatal(err)
				}
			case "id":
				if err := os.WriteFile(filepath.Join(data, "rooms", "room_1.json"), []byte("collision"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(trash, "entry_1", "payload"), source); err != nil {
					t.Fatal(err)
				}
			case "payload":
				if err := os.WriteFile(filepath.Join(trash, "entry_1", "payload", "room.json"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.kind == "hash" {
				p.ExpectedInventoryHash = "sha256:wrong"
			}
			next, err := r.PreviewRestore("entry_1")
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind != "hash" && next.RestoreAvailable {
				t.Fatalf("unexpected restorable %+v", next)
			}
			if _, err := r.Restore("entry_1", p.ExpectedInventoryHash, "restore_1"); !errors.Is(err, ErrTrashChanged) {
				t.Fatalf("restore conflict: %v", err)
			}
		})
	}
	if _, err := func() (TrashRestorePreview, error) {
		r, _, _, _ := sealedRoomFixture(t)
		return r.PreviewRestore("missing")
	}(); !errors.Is(err, ErrTrashMissing) {
		t.Fatalf("missing: %v", err)
	}
}

func TestTrashRestoreRenameInterruption(t *testing.T) {
	r, source, trash, _ := sealedRoomFixture(t)
	p, err := r.PreviewRestore("entry_1")
	if err != nil {
		t.Fatal(err)
	}
	r.beforeRestoreStage = func(stage TrashRestoreStage) error {
		if stage == TrashRestoreAfterRename {
			return errors.New("injected interruption")
		}
		return nil
	}
	_, err = r.Restore("entry_1", p.ExpectedInventoryHash, "restore_1")
	var restoreErr *TrashRestoreError
	if !errors.As(err, &restoreErr) || !restoreErr.SourceMoved {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, "entry_1", "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("payload=%v", err)
	}
	if classification, err := r.Reconcile("entry_1"); err != nil || classification != "restored_unconfirmed" {
		t.Fatalf("reconcile=%s %v", classification, err)
	}
}

func TestTrashRenameNoReplace(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "target")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := trashRenameNoReplace(source, target); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected collision, got %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "target" {
		t.Fatalf("target overwritten: %q %v", data, err)
	}
}
