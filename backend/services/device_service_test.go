package services

import (
	"os"
	"path/filepath"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestDeviceServiceCreateUpdateRestartReadback(t *testing.T) {
	rooms := t.TempDir()
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "bedroom", Name: "Bedroom"}); err != nil {
		t.Fatal(err)
	}
	service := NewDeviceServiceWithRoomsDir(rooms)
	device := models.Device{ID: "lamp", RoomID: "bedroom", Name: "Lamp", DeviceType: "light", PrimaryAdapter: "ir"}
	if _, err := service.CreateDevice("bedroom", device); err != nil {
		t.Fatal(err)
	}
	device.Name = "Updated Lamp"
	if _, err := service.UpdateDevice("bedroom", "lamp", device); err != nil {
		t.Fatal(err)
	}
	got, err := NewDeviceServiceWithRoomsDir(rooms).GetDevice("bedroom", "lamp")
	if err != nil || got.Name != device.Name || got.ID != device.ID {
		t.Fatalf("restart-like readback: got %+v, err %v", got, err)
	}
}

func TestDeviceServiceUpdateRejectsCorruptExistingJSON(t *testing.T) {
	rooms := t.TempDir()
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "bedroom", Name: "Bedroom"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rooms, "bedroom", "devices", "lamp", "device.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewDeviceServiceWithRoomsDir(rooms).UpdateDevice("bedroom", "lamp", models.Device{ID: "lamp", RoomID: "bedroom", Name: "New", DeviceType: "light", PrimaryAdapter: "ir"})
	serviceErr, ok := AsServiceError(err)
	if !ok || serviceErr.Kind != ErrorInternal {
		t.Fatalf("expected corrupt-device internal error, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{" {
		t.Fatalf("corrupt JSON overwritten: %q, %v", data, err)
	}
}

func TestDeviceOrphanFromFailedWriteDetectedByDataHealth(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	rooms := filepath.Join(root, "rooms")
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "bedroom", Name: "Bedroom"}); err != nil {
		t.Fatal(err)
	}
	// A failed pre-rename Device write can leave this directory but no JSON.
	if err := os.MkdirAll(filepath.Join(rooms, "bedroom", "devices", "lamp"), 0755); err != nil {
		t.Fatal(err)
	}
	issues, err := NewDataHealthService(repositories.NewDataHealthRepository(root)).Scan()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"missing_device_json:rooms/bedroom/devices/lamp": false, "orphan_device:rooms/bedroom/devices/lamp": false}
	for _, issue := range issues {
		if _, ok := want[issue.ID]; ok {
			want[issue.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("Data Health did not detect %s: %+v", id, issues)
		}
	}
}

func TestDeviceServiceAdapterDirectoryFailureLeavesReadableDevice(t *testing.T) {
	rooms := t.TempDir()
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "bedroom", Name: "Bedroom"}); err != nil {
		t.Fatal(err)
	}
	deviceDir := filepath.Join(rooms, "bedroom", "devices", "lamp")
	if err := os.MkdirAll(deviceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deviceDir, "adapters"), []byte("obstruction"), 0644); err != nil {
		t.Fatal(err)
	}
	device := models.Device{ID: "lamp", RoomID: "bedroom", Name: "Lamp", DeviceType: "light", PrimaryAdapter: "ir"}
	_, err := NewDeviceServiceWithRoomsDir(rooms).CreateDevice("bedroom", device)
	serviceErr, ok := AsServiceError(err)
	if !ok || serviceErr.Kind != ErrorInternal || serviceErr.Message != "failed to create device adapter directory" {
		t.Fatalf("expected adapter directory failure, got %v", err)
	}
	got, err := NewDeviceServiceWithRoomsDir(rooms).GetDevice("bedroom", "lamp")
	if err != nil || got.Name != device.Name {
		t.Fatalf("device.json should remain readable after adapter directory failure: %+v, %v", got, err)
	}
}
