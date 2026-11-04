package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func trashFixture(t *testing.T) (*TrashRepository, string, string, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	state := filepath.Join(base, "state")
	trash := filepath.Join(base, "trash")
	for _, dir := range []string{data, filepath.Join(data, "rooms"), state, trash} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := NewTrashRepository(TrashConfig{Dir: trash, RuntimeDataDir: data, StateDir: state, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(data, "rooms", "item.json")
	if err := os.WriteFile(source, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	return repo, source, trash, data
}

func trashRequest() TrashMoveRequest {
	return TrashMoveRequest{EntryID: "entry_1", SourceRelativePath: "rooms/item.json", OriginalID: "item", TargetType: "room", Reason: "operator delete", OperationType: "delete", OperationID: "op_1"}
}

func TestTrashMoveAndReadback(t *testing.T) {
	r, source, root, _ := trashFixture(t)
	fixed := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return fixed }
	result, err := r.Move(trashRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !result.EntryCreated || !result.SourceMoved || !result.Verified || result.Entry.Status != "sealed" {
		t.Fatalf("result: %+v", result)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "entry_1", "payload")); err != nil {
		t.Fatal(err)
	}
	entry, err := r.Load("entry_1")
	if err != nil || entry.DeletedAt == nil || !entry.DeletedAt.Equal(fixed) || entry.EligibleAfter == nil || !entry.EligibleAfter.Equal(fixed.AddDate(0, 0, 30)) || !entry.RestoreAvailable || entry.BeforeInventory.FileCount != 1 {
		t.Fatalf("entry: %+v %v", entry, err)
	}
	if got, err := r.Reconcile("entry_1"); err != nil || got != "moved_unconfirmed" {
		t.Fatalf("reconcile: %s %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, "entry_1", "metadata.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("metadata mode: %v %v", info, err)
	}
}

func TestTrashMoveDirectoryInventory(t *testing.T) {
	r, source, _, data := trashFixture(t)
	dir := filepath.Join(data, "rooms", "device")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device.json"), []byte(`{"id":"x"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	request := trashRequest()
	request.SourceRelativePath = "rooms/device"
	request.EntryID = "device_1"
	result, err := r.Move(request)
	if err != nil || !result.Verified || result.Entry.BeforeInventory.Kind != "directory" || result.Entry.BeforeInventory.FileCount != 1 {
		t.Fatalf("result: %+v %v", result, err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("unrelated source changed: %v", err)
	}
}

func TestTrashBoundaryAndCollision(t *testing.T) {
	r, source, root, data := trashFixture(t)
	if _, err := NewTrashRepository(TrashConfig{Dir: data, RuntimeDataDir: data, StateDir: filepath.Dir(root), RetentionDays: 30}); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("overlap: %v", err)
	}
	if _, err := NewTrashRepository(TrashConfig{Dir: root, RuntimeDataDir: data, StateDir: filepath.Dir(root), RetentionDays: 0}); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("retention: %v", err)
	}
	link := filepath.Join(filepath.Dir(root), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTrashRepository(TrashConfig{Dir: link, RuntimeDataDir: data, StateDir: filepath.Join(filepath.Dir(root), "state"), RetentionDays: 30}); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("symlink root: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "entry_1"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Move(trashRequest()); !errors.Is(err, ErrTrashCollision) {
		t.Fatalf("collision: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source changed on collision: %v", err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Move(TrashMoveRequest{EntryID: "entry_2", SourceRelativePath: "rooms/item.json", OriginalID: "item", TargetType: "room", Reason: "delete", OperationType: "delete", OperationID: "op_1"}); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("mode: %v", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	r.uid++
	if _, err := r.Move(TrashMoveRequest{EntryID: "entry_2", SourceRelativePath: "rooms/item.json", OriginalID: "item", TargetType: "room", Reason: "delete", OperationType: "delete", OperationID: "op_1"}); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("owner: %v", err)
	}
}

func TestTrashCrossDeviceAndUnsafeSource(t *testing.T) {
	r, source, _, data := trashFixture(t)
	r.sameDevice = func(os.FileInfo, os.FileInfo) bool { return false }
	if _, err := r.Move(trashRequest()); !errors.Is(err, ErrTrashCrossDevice) {
		t.Fatalf("cross-device: %v", err)
	}
	r.sameDevice = trashSameDevice
	request := trashRequest()
	request.SourceRelativePath = "../outside"
	if _, err := r.Move(request); !errors.Is(err, ErrTrashBoundary) {
		t.Fatalf("traversal: %v", err)
	}
	if err := os.Symlink(source, filepath.Join(data, "rooms", "link")); err != nil {
		t.Fatal(err)
	}
	request.SourceRelativePath = "rooms/link"
	if _, err := r.Move(request); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}

func TestTrashInterruptionBeforeAndAfterRename(t *testing.T) {
	for _, tc := range []struct {
		stage          TrashMoveStage
		moved          bool
		classification string
	}{{TrashStageBeforeRename, false, "unchanged"}, {TrashStageAfterRename, true, "moved_unconfirmed"}} {
		t.Run(string(tc.stage), func(t *testing.T) {
			r, source, _, _ := trashFixture(t)
			injected := errors.New("injected interruption")
			r.beforeStage = func(stage TrashMoveStage) error {
				if stage == tc.stage {
					return injected
				}
				return nil
			}
			result, err := r.Move(trashRequest())
			var moveErr *TrashMoveError
			if !errors.As(err, &moveErr) || !errors.Is(err, injected) || result.SourceMoved != tc.moved || moveErr.SourceMoved != tc.moved {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			entry, err := r.Load("entry_1")
			if err != nil || entry.Status != "prepared" {
				t.Fatalf("prepared: %+v %v", entry, err)
			}
			got, err := r.Reconcile("entry_1")
			if err != nil || got != tc.classification {
				t.Fatalf("reconcile: %s %v", got, err)
			}
			_, sourceErr := os.Lstat(source)
			if tc.moved && !errors.Is(sourceErr, os.ErrNotExist) {
				t.Fatalf("source remains: %v", sourceErr)
			}
		})
	}
}

func TestTrashParentSyncFailureAndHashMismatch(t *testing.T) {
	t.Run("parent sync", func(t *testing.T) {
		r, source, _, _ := trashFixture(t)
		injected := errors.New("injected parent sync failure")
		originalSync := r.syncDir
		r.syncDir = func(path string) error {
			if path == filepath.Dir(source) {
				return injected
			}
			return originalSync(path)
		}
		result, err := r.Move(trashRequest())
		var moveErr *TrashMoveError
		if !errors.As(err, &moveErr) || moveErr.Stage != TrashStageParentSync || !result.SourceMoved || !errors.Is(err, injected) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if got, err := r.Reconcile("entry_1"); err != nil || got != "moved_unconfirmed" {
			t.Fatalf("reconcile: %s %v", got, err)
		}
	})
	t.Run("hash mismatch", func(t *testing.T) {
		r, _, root, _ := trashFixture(t)
		r.beforeStage = func(stage TrashMoveStage) error {
			if stage == TrashStageReadback {
				return os.WriteFile(filepath.Join(root, "entry_1", "payload"), []byte("changed"), 0600)
			}
			return nil
		}
		result, err := r.Move(trashRequest())
		var moveErr *TrashMoveError
		if !errors.As(err, &moveErr) || moveErr.Stage != TrashStageReadback || !result.SourceMoved || !errors.Is(err, ErrTrashChanged) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if got, err := r.Reconcile("entry_1"); err != nil || got != "ambiguous" {
			t.Fatalf("reconcile: %s %v", got, err)
		}
	})
}

func TestTrashLoadMissingAndCorrupt(t *testing.T) {
	r, _, root, _ := trashFixture(t)
	if _, err := r.Load("missing"); !errors.Is(err, ErrTrashMissing) {
		t.Fatalf("missing: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "broken"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "metadata.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load("broken"); !errors.Is(err, ErrTrashCorrupt) {
		t.Fatalf("corrupt: %v", err)
	}
}

type failSecondTrashWriter struct {
	calls      int
	underlying JSONWriter
	failure    error
}

func (w *failSecondTrashWriter) WriteJSON(path string, value any, mode os.FileMode) error {
	w.calls++
	if w.calls == 2 {
		return w.failure
	}
	return w.underlying.WriteJSON(path, value, mode)
}

func TestTrashSealFailureLeavesPreparedEntry(t *testing.T) {
	r, _, _, _ := trashFixture(t)
	injected := errors.New("injected seal failure")
	r.writer = &failSecondTrashWriter{underlying: NewAtomicJSONWriter(), failure: injected}
	result, err := r.Move(trashRequest())
	var moveErr *TrashMoveError
	if !errors.As(err, &moveErr) || moveErr.Stage != TrashStageSeal || !moveErr.SourceMoved || result.Verified || !errors.Is(err, injected) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	entry, err := r.Load("entry_1")
	if err != nil || entry.Status != "prepared" {
		t.Fatalf("entry: %+v %v", entry, err)
	}
	if got, err := r.Reconcile("entry_1"); err != nil || got != "moved_unconfirmed" {
		t.Fatalf("reconcile: %s %v", got, err)
	}
}

func TestTrashSourceChangedBeforeRename(t *testing.T) {
	r, source, _, _ := trashFixture(t)
	r.beforeStage = func(stage TrashMoveStage) error {
		if stage == TrashStageBeforeRename {
			return os.WriteFile(source, []byte("changed"), 0600)
		}
		return nil
	}
	result, err := r.Move(trashRequest())
	var moveErr *TrashMoveError
	if !errors.As(err, &moveErr) || moveErr.SourceMoved || !errors.Is(err, ErrTrashChanged) || result.SourceMoved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got, err := r.Reconcile("entry_1"); err != nil || got != "ambiguous" {
		t.Fatalf("reconcile: %s %v", got, err)
	}
}

func TestTrashRejectsNestedSourceSymlink(t *testing.T) {
	r, _, _, data := trashFixture(t)
	dir := filepath.Join(data, "rooms", "device")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(data, "rooms", "item.json"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	request := trashRequest()
	request.SourceRelativePath = "rooms/device"
	result, err := r.Move(request)
	if err == nil || result.EntryCreated || result.SourceMoved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("source moved: %v", err)
	}
}

func TestTrashConcurrentSameEntry(t *testing.T) {
	r, _, _, _ := trashFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := r.Move(trashRequest()); results <- err }()
	}
	wg.Wait()
	close(results)
	success, collision := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrTrashCollision) {
			collision++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || collision != 1 {
		t.Fatalf("success=%d collision=%d", success, collision)
	}
}

func TestTrashListEmptySortedAndCorrupt(t *testing.T) {
	r, _, root, data := trashFixture(t)
	list, err := r.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("empty list: %+v %v", list, err)
	}
	first := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return first }
	if _, err := r.Move(trashRequest()); err != nil {
		t.Fatal(err)
	}
	secondSource := filepath.Join(data, "rooms", "second.json")
	if err := os.WriteFile(secondSource, []byte(`{"id":"second"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return first.Add(time.Hour) }
	request := trashRequest()
	request.EntryID = "entry_2"
	request.SourceRelativePath = "rooms/second.json"
	request.TargetType = "device"
	request.OriginalID = "second"
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	list, err = r.List()
	if err != nil || len(list) != 2 || list[0].ID != "entry_2" || list[1].ID != "entry_1" {
		t.Fatalf("sorted list: %+v %v", list, err)
	}
	if err := os.Mkdir(filepath.Join(root, "broken"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "metadata.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.List(); !errors.Is(err, ErrTrashCorrupt) {
		t.Fatalf("corrupt list: %v", err)
	}
}

func TestTrashMovedRoomInvisibleToRoomRepository(t *testing.T) {
	r, _, _, data := trashFixture(t)
	roomDir := filepath.Join(data, "rooms", "bedroom")
	if err := os.Mkdir(roomDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roomDir, "room.json"), []byte(`{"id":"bedroom","name":"Bedroom"}`), 0600); err != nil {
		t.Fatal(err)
	}
	rooms := NewRoomRepositoryWithBaseDir(filepath.Join(data, "rooms"))
	before, err := rooms.List()
	if err != nil || len(before) != 1 {
		t.Fatalf("before move: %+v %v", before, err)
	}
	request := trashRequest()
	request.EntryID = "room_entry"
	request.SourceRelativePath = "rooms/bedroom"
	request.OriginalID = "bedroom"
	if _, err := r.Move(request); err != nil {
		t.Fatal(err)
	}
	after, err := rooms.List()
	if err != nil || len(after) != 0 {
		t.Fatalf("trashed room visible: %+v %v", after, err)
	}
}
