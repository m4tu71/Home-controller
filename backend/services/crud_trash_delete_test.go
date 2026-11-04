package services

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"backend/repositories"
)

type crudDeleteFixture struct {
	dataRoot  string
	trashRoot string
	state     *repositories.MaintenanceState
	trash     *repositories.TrashRepository
	service   *CRUDTrashDeleteService
}

func newCRUDDeleteFixture(t *testing.T) crudDeleteFixture {
	t.Helper()
	base := t.TempDir()
	dataRoot := filepath.Join(base, "runtime")
	stateRoot := filepath.Join(base, "state")
	trashRoot := filepath.Join(base, "trash")
	for _, dir := range []string{
		dataRoot,
		filepath.Join(dataRoot, "rooms"),
		stateRoot,
		filepath.Join(stateRoot, "plans"),
		filepath.Join(stateRoot, "operations"),
		trashRoot,
	} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{
		Dir: stateRoot, RuntimeDataDir: dataRoot, TrashDir: trashRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{
		Dir: trashRoot, RuntimeDataDir: dataRoot, StateDir: stateRoot, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	return crudDeleteFixture{
		dataRoot: dataRoot, trashRoot: trashRoot, state: state, trash: trash,
		service: NewCRUDTrashDeleteService(state.Operations(), trash, dataRoot, trashRoot),
	}
}

func TestCRUDTrashDeleteMovesOnlyTargetAndPersistsVerifiedOperation(t *testing.T) {
	f := newCRUDDeleteFixture(t)
	cases := []struct {
		name, targetType, originalID, rel string
	}{
		{"room metadata", "room", "empty_room", "rooms/empty_room/room.json"},
		{"device metadata", "device", "lamp", "rooms/bedroom/devices/lamp/device.json"},
		{"IR Command", "ir_command", "power", "rooms/bedroom/devices/lamp/adapters/ir/commands/power.json"},
		{"IR State", "ir_state", "cool", "rooms/bedroom/devices/ac/adapters/ir/states/cool.json"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := filepath.Join(f.dataRoot, filepath.FromSlash(tc.rel))
			if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
				t.Fatal(err)
			}
			payload := []byte(`{"id":"` + tc.originalID + `","preserve":"bytes"}`)
			if err := os.WriteFile(source, payload, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.targetType == "device" {
				other := filepath.Join(filepath.Dir(source), "adapters", "ir", "ir_config.json")
				if err := os.MkdirAll(filepath.Dir(other), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(other, []byte(`{"ir_remote_type":"command"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := f.service.DeleteFile(tc.targetType, tc.originalID, tc.rel)
			if err != nil || result.Status != string(MaintenanceVerified) || result.OperationID == "" || result.TrashEntryID == "" {
				t.Fatalf("delete result=%+v err=%v", result, err)
			}
			if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("source still exists or stat failed: %v", err)
			}
			trashPayload := filepath.Join(f.trashRoot, result.TrashEntryID, "payload")
			got, err := os.ReadFile(trashPayload)
			if err != nil || string(got) != string(payload) {
				t.Fatalf("Trash payload=%q err=%v", got, err)
			}
			entry, err := f.trash.Load(result.TrashEntryID)
			if err != nil || entry.Status != "sealed" || !entry.RestoreAvailable || entry.OperationID != result.OperationID || entry.OperationType != "delete" || entry.TargetType != tc.targetType {
				t.Fatalf("Trash entry=%+v err=%v", entry, err)
			}
			operation, err := f.state.Operations().Load(result.OperationID)
			if err != nil || operation.Type != "delete" || operation.Status != string(MaintenanceVerified) || len(operation.Actions) != 1 || operation.Actions[0].Status != string(MaintenanceActionVerified) {
				t.Fatalf("operation=%+v err=%v", operation, err)
			}
			preview, err := f.trash.PreviewRestore(result.TrashEntryID)
			if err != nil || !preview.RestoreAvailable {
				t.Fatalf("restore preview=%+v err=%v", preview, err)
			}
			if tc.targetType == "device" {
				if _, err := os.Stat(filepath.Join(filepath.Dir(source), "adapters", "ir", "ir_config.json")); err != nil {
					t.Fatalf("Device deletion unexpectedly moved sibling IR metadata: %v", err)
				}
			}
			_ = i
		})
	}
}

func TestCRUDTrashDeleteSerializesConcurrentSameTarget(t *testing.T) {
	f := newCRUDDeleteFixture(t)
	rel := "rooms/bedroom/devices/lamp/adapters/ir/commands/power.json"
	source := filepath.Join(f.dataRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"id":"power"}`), 0600); err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	results := make([]CRUDDeleteResult, 0, attempts)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.service.DeleteFile("ir_command", "power", rel)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
				results = append(results, result)
			}
		}()
	}
	wg.Wait()
	operations, err := f.state.Operations().List()
	if err != nil || successes != 1 || len(operations) != 1 || operations[0].Status != string(MaintenanceVerified) {
		t.Fatalf("successes=%d operations=%+v err=%v", successes, operations, err)
	}
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
}

func TestCRUDTrashDeleteRejectsUnsafeTargets(t *testing.T) {
	f := newCRUDDeleteFixture(t)
	for _, tc := range []struct{ targetType, id, rel string }{
		{"room", "room", "rooms/room/devices/device/device.json"},
		{"device", "device", "rooms/../device.json"},
		{"ir_command", "command", "rooms/room/devices/device/adapters/ir/states/command.json"},
		{"ir_state", "state", "rooms/room/devices/device/adapters/ir/states/../state.json"},
	} {
		if _, err := f.service.DeleteFile(tc.targetType, tc.id, tc.rel); err == nil {
			t.Errorf("DeleteFile(%q, %q, %q) unexpectedly succeeded", tc.targetType, tc.id, tc.rel)
		}
	}
}
