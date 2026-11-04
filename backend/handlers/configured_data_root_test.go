package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestConfigureDataRootUsesSelectedRoomsDirectory(t *testing.T) {
	first := filepath.Join(t.TempDir(), "data", "rooms")
	second := filepath.Join(t.TempDir(), "data", "rooms")
	if err := os.MkdirAll(first, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ConfigureDataRoot("") })

	ConfigureDataRoot(first)
	request := httptest.NewRequest(http.MethodPost, "/api/controller/rooms", strings.NewReader(`{"id":"selected","name":"Selected"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	RoomsHandler(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := repositories.NewRoomRepositoryWithBaseDir(first).Load("selected"); err != nil {
		t.Fatalf("selected root readback: %v", err)
	}
	if _, err := repositories.NewRoomRepositoryWithBaseDir(second).Load("selected"); !os.IsNotExist(err) {
		t.Fatalf("unexpected room in second root: %v", err)
	}
	if _, err := deviceService.CreateDevice("selected", models.Device{ID: "lamp", Name: "Lamp", RoomID: "selected", DeviceType: "light", PrimaryAdapter: "ir"}); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if _, err := irAdapterService.SaveConfig("selected", "lamp", models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38}); err != nil {
		t.Fatalf("save IR config: %v", err)
	}
	if _, err := repositories.NewDeviceRepositoryWithRoomsDir(first).Load("selected", "lamp"); err != nil {
		t.Fatalf("selected device readback: %v", err)
	}
	if _, err := repositories.NewIRAdapterRepositoryWithRoomsDir(first).LoadConfig("selected", "lamp"); err != nil {
		t.Fatalf("selected IR config readback: %v", err)
	}

	ConfigureDataRoot(second)
	response = httptest.NewRecorder()
	RoomsHandler(response, httptest.NewRequest(http.MethodGet, "/api/controller/rooms", nil))
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("second root list status=%d body=%s", response.Code, response.Body.String())
	}
}
