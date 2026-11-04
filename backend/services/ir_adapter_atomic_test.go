package services

import (
	"os"
	"path/filepath"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestIRAdapterServiceRejectsCorruptConfigAndSchemaOverwrite(t *testing.T) {
	for _, family := range []string{"config", "schema"} {
		t.Run(family, func(t *testing.T) {
			rooms := t.TempDir()
			roomRepo := repositories.NewRoomRepositoryWithBaseDir(rooms)
			deviceRepo := repositories.NewDeviceRepositoryWithRoomsDir(rooms)
			irRepo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
			if err := roomRepo.Save(models.Room{ID: "room", Name: "Room"}); err != nil {
				t.Fatal(err)
			}
			if err := deviceRepo.Save(models.Device{ID: "device", RoomID: "room", Name: "Device", DeviceType: "light", PrimaryAdapter: "ir"}); err != nil {
				t.Fatal(err)
			}
			service := IRAdapterService{roomRepository: roomRepo, deviceRepository: deviceRepo, irAdapterRepository: irRepo}
			path := filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "ir_config.json")
			if family == "schema" {
				if err := irRepo.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "state_schema.json")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
				t.Fatal(err)
			}
			var err error
			if family == "config" {
				_, err = service.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38})
			} else {
				_, err = service.SaveStateSchema("room", "device", models.IRStateSchema{"power": {Type: models.IRStateFieldTypeBoolean}})
			}
			serviceErr, ok := AsServiceError(err)
			if !ok || serviceErr.Kind != ErrorInternal {
				t.Fatalf("expected corrupt JSON read failure, got %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "{" {
				t.Fatalf("corrupt JSON overwritten: %q, %v", data, err)
			}
		})
	}
}

func TestIRAdapterServiceRejectsCorruptCommandAndStateOverwrite(t *testing.T) {
	for _, family := range []string{"command", "state"} {
		t.Run(family, func(t *testing.T) {
			rooms := t.TempDir()
			roomRepo := repositories.NewRoomRepositoryWithBaseDir(rooms)
			deviceRepo := repositories.NewDeviceRepositoryWithRoomsDir(rooms)
			irRepo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
			if err := roomRepo.Save(models.Room{ID: "room", Name: "Room"}); err != nil {
				t.Fatal(err)
			}
			if err := deviceRepo.Save(models.Device{ID: "device", RoomID: "room", Name: "Device", DeviceType: "light", PrimaryAdapter: "ir"}); err != nil {
				t.Fatal(err)
			}
			remoteType := models.IRRemoteTypeCommand
			path := filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "commands", "power.json")
			if family == "state" {
				remoteType = models.IRRemoteTypeStateful
				path = filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "states", "power.json")
			}
			if err := irRepo.SaveConfig("room", "device", models.IRConfig{IRRemoteType: remoteType, CarrierKHz: 38}); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
				t.Fatal(err)
			}
			service := IRAdapterService{roomRepository: roomRepo, deviceRepository: deviceRepo, irAdapterRepository: irRepo}
			var err error
			if family == "command" {
				_, err = service.UpdateCommand("room", "device", "power", models.IRCommandWriteRequest{})
			} else {
				_, err = service.UpdateState("room", "device", "power", models.IRStateWriteRequest{})
			}
			serviceErr, ok := AsServiceError(err)
			if !ok || serviceErr.Kind != ErrorInternal {
				t.Fatalf("expected corrupt JSON read failure, got %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "{" {
				t.Fatalf("corrupt JSON overwritten: %q, %v", data, err)
			}
		})
	}
}
