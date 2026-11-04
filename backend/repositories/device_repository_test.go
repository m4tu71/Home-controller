package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/models"
)

func TestDeviceRepositoryAtomicSaveReadback(t *testing.T) {
	rooms := t.TempDir()
	repo := NewDeviceRepositoryWithRoomsDir(rooms)
	device := models.Device{ID: "lamp", RoomID: "bedroom", Name: "Lamp", DeviceType: "light", PrimaryAdapter: "ir"}
	for _, name := range []string{"Lamp", "Updated Lamp"} {
		device.Name = name
		if err := repo.Save(device); err != nil {
			t.Fatal(err)
		}
		got, err := NewDeviceRepositoryWithRoomsDir(rooms).Load("bedroom", "lamp")
		if err != nil || !reflect.DeepEqual(got, device) {
			t.Fatalf("reconstructed repository readback: got %+v, err %v; want %+v", got, err, device)
		}
	}
}

func TestDeviceRepositoryAtomicFailureBoundary(t *testing.T) {
	stages := []AtomicWriteStage{AtomicWriteCreateTemp, AtomicWriteWrite, AtomicWriteChmod, AtomicWriteFileSync, AtomicWriteFileClose, AtomicWriteRename, AtomicWriteParentSync}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			rooms := t.TempDir()
			repo := NewDeviceRepositoryWithRoomsDir(rooms)
			old := models.Device{ID: "lamp", RoomID: "bedroom", Name: "Old"}
			next := models.Device{ID: "lamp", RoomID: "bedroom", Name: "New"}
			if err := repo.Save(old); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected device write failure")
			repo.writer = &AtomicJSONWriter{beforeStage: func(current AtomicWriteStage) error {
				if current == stage {
					return injected
				}
				return nil
			}}
			err := repo.Save(next)
			var writeErr *AtomicWriteError
			if !errors.As(err, &writeErr) || !errors.Is(err, injected) || writeErr.Stage != stage || writeErr.TargetReplaced != (stage == AtomicWriteParentSync) {
				t.Fatalf("unexpected write error: %v", err)
			}
			want := old
			if stage == AtomicWriteParentSync {
				want = next
			}
			got, err := NewDeviceRepositoryWithRoomsDir(rooms).Load("bedroom", "lamp")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("readback after %s: got %+v, err %v; want %+v", stage, got, err, want)
			}
			entries, err := os.ReadDir(filepath.Join(rooms, "bedroom", "devices", "lamp"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "device.json" {
				t.Fatalf("unexpected temporary files: %v, %v", entries, err)
			}
		})
	}
}

func TestDeviceRepositoryFailedCreateLeavesOrphanDirectory(t *testing.T) {
	rooms := t.TempDir()
	repo := NewDeviceRepositoryWithRoomsDir(rooms)
	repo.writer = &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		if stage == AtomicWriteWrite {
			return errors.New("injected")
		}
		return nil
	}}
	if err := repo.Save(models.Device{ID: "lamp", RoomID: "bedroom", Name: "Lamp"}); err == nil {
		t.Fatal("expected save failure")
	}
	if _, err := NewDeviceRepositoryWithRoomsDir(rooms).Load("bedroom", "lamp"); !os.IsNotExist(err) {
		t.Fatalf("partial device became readable: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(rooms, "bedroom", "devices", "lamp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected empty orphan device directory: %v, %v", entries, err)
	}
}
