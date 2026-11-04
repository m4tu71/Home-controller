package services

import (
	"os"
	"path/filepath"
	"testing"

	"backend/models"
)

func TestRoomServiceCreateUpdateRestartReadback(t *testing.T) {
	base := t.TempDir()
	service := NewRoomServiceWithBaseDir(base)
	room := models.Room{ID: "bedroom", Name: "Bedroom"}
	if _, err := service.CreateRoom(room); err != nil {
		t.Fatal(err)
	}
	room.Name = "Updated"
	if _, err := service.UpdateRoom(room.ID, room); err != nil {
		t.Fatal(err)
	}
	got, err := NewRoomServiceWithBaseDir(base).GetRoom(room.ID)
	if err != nil || got != room {
		t.Fatalf("restart-like readback: got %+v, err %v", got, err)
	}
}

func TestRoomServiceUpdateRejectsCorruptExistingJSON(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "bedroom", "room.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("{")
	if err := os.WriteFile(path, corrupt, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := NewRoomServiceWithBaseDir(base).UpdateRoom("bedroom", models.Room{ID: "bedroom", Name: "Updated"})
	serviceErr, ok := AsServiceError(err)
	if !ok || serviceErr.Kind != ErrorInternal {
		t.Fatalf("expected corrupt-room internal error, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(corrupt) {
		t.Fatalf("corrupt source was overwritten: %q, %v", got, err)
	}
}
