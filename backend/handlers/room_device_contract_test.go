package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const (
	roomCreateBody   = `{"id":"newroom","name":"New Room"}`
	roomUpdateBody   = `{"id":"room","name":"Updated Room"}`
	deviceCreateBody = `{"id":"newdevice","name":"New Device","room_id":"room","device_type":"light","primary_adapter":"ir"}`
	deviceUpdateBody = `{"id":"device","name":"Updated Device","room_id":"room","device_type":"light","primary_adapter":"ir"}`
)

func setRoomDeviceFixture(t *testing.T, withDevice bool) {
	t.Helper()
	baseDir := t.TempDir()
	dataRoot := filepath.Join(baseDir, "runtime")
	roomsDir := filepath.Join(dataRoot, "rooms")
	stateDir := filepath.Join(baseDir, "state")
	trashDir := filepath.Join(baseDir, "trash")
	for _, dir := range []string{dataRoot, roomsDir, stateDir, filepath.Join(stateDir, "plans"), filepath.Join(stateDir, "operations"), trashDir} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: stateDir, RuntimeDataDir: dataRoot, TrashDir: trashDir})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{Dir: trashDir, RuntimeDataDir: dataRoot, StateDir: stateDir, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	deletes := services.NewCRUDTrashDeleteService(state.Operations(), trash, dataRoot, trashDir)
	oldRooms, oldDevices := roomService, deviceService
	roomService = services.NewRoomServiceWithBaseDir(roomsDir).WithTrashDeletes(deletes)
	deviceService = services.NewDeviceServiceWithRoomsDir(roomsDir).WithTrashDeletes(deletes)
	t.Cleanup(func() { roomService, deviceService = oldRooms, oldDevices })
	if _, err := roomService.CreateRoom(models.Room{ID: "room", Name: "Fixture Room"}); err != nil {
		t.Fatalf("create fixture Room: %v", err)
	}
	if withDevice {
		if _, err := deviceService.CreateDevice("room", models.Device{
			ID: "device", Name: "Fixture Device", RoomID: "room", DeviceType: "light", PrimaryAdapter: "ir",
		}); err != nil {
			t.Fatalf("create fixture Device: %v", err)
		}
	}
}

func roomDeviceRequest(t *testing.T, method, path, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	RoomsHandler(recorder, request)
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", recorder.Header().Get("Content-Type"))
	}
	return recorder
}

func TestRoomDeviceRouteStatusAndErrorContract(t *testing.T) {
	tests := []struct {
		name, method, path, body, contentType, wantError, wantAllow string
		withDevice                                                  bool
		wantStatus                                                  int
	}{
		{"list rooms", "GET", "/api/controller/rooms", "", "", "", "", false, 200},
		{"create room", "POST", "/api/controller/rooms", roomCreateBody, "application/json", "", "", false, 201},
		{"get room", "GET", "/api/controller/rooms/room", "", "", "", "", false, 200},
		{"update room", "PUT", "/api/controller/rooms/room", roomUpdateBody, "application/json", "", "", false, 200},
		{"delete room", "DELETE", "/api/controller/rooms/room", "", "", "", "", false, 200},
		{"room duplicate", "POST", "/api/controller/rooms", `{"id":"room","name":"Duplicate"}`, "application/json", "room already exists", "", false, 409},
		{"room has device", "DELETE", "/api/controller/rooms/room", "", "", "room has devices", "", true, 409},
		{"room missing", "GET", "/api/controller/rooms/missing", "", "", "room not found", "", false, 404},
		{"room invalid ID", "GET", "/api/controller/rooms/INVALID", "", "", "invalid room id", "", false, 400},
		{"room collection method", "PATCH", "/api/controller/rooms", "", "", "method not allowed", "GET, POST", false, 405},
		{"room item method", "POST", "/api/controller/rooms/room", "", "", "method not allowed", "GET, PUT, DELETE", false, 405},
		{"list devices", "GET", "/api/controller/rooms/room/devices", "", "", "", "", true, 200},
		{"create device", "POST", "/api/controller/rooms/room/devices", deviceCreateBody, "application/json", "", "", true, 201},
		{"get device", "GET", "/api/controller/rooms/room/devices/device", "", "", "", "", true, 200},
		{"update device", "PUT", "/api/controller/rooms/room/devices/device", deviceUpdateBody, "application/json", "", "", true, 200},
		{"delete device", "DELETE", "/api/controller/rooms/room/devices/device", "", "", "", "", true, 200},
		{"device duplicate", "POST", "/api/controller/rooms/room/devices", `{"id":"device","name":"Duplicate","room_id":"room","device_type":"light","primary_adapter":"ir"}`, "application/json", "device already exists", "", true, 409},
		{"device missing", "GET", "/api/controller/rooms/room/devices/missing", "", "", "device not found", "", true, 404},
		{"parent room missing", "GET", "/api/controller/rooms/missing/devices/device", "", "", "room not found", "", true, 404},
		{"device invalid ID", "GET", "/api/controller/rooms/room/devices/INVALID", "", "", "invalid device id", "", true, 400},
		{"device validation", "POST", "/api/controller/rooms/room/devices", `{"id":"newdevice","name":"N","device_type":"invalid","primary_adapter":"ir"}`, "application/json", "invalid device type", "", true, 400},
		{"device collection method", "PATCH", "/api/controller/rooms/room/devices", "", "", "method not allowed", "GET, POST", true, 405},
		{"device item method", "POST", "/api/controller/rooms/room/devices/device", "", "", "method not allowed", "GET, PUT, DELETE", true, 405},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRoomDeviceFixture(t, test.withDevice)
			recorder := roomDeviceRequest(t, test.method, test.path, test.body, test.contentType)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("Allow"); got != test.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, test.wantAllow)
			}
			if test.wantError != "" {
				var body struct {
					OK    bool   `json:"ok"`
					Error string `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body.OK || body.Error != test.wantError {
					t.Fatalf("error body = %#v, want %q", body, test.wantError)
				}
			}
			if test.method == "DELETE" && recorder.Code == 200 {
				var body struct {
					TrashEntryID string `json:"trash_entry_id"`
					OperationID  string `json:"operation_id"`
					Status       string `json:"status"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.TrashEntryID == "" || body.OperationID == "" || body.Status != "verified" {
					t.Fatalf("delete result missing Trash/operation verification: %+v err=%v", body, err)
				}
			}
		})
	}
}

func TestRoomDeviceWriteRequestBoundary(t *testing.T) {
	writes := []struct {
		name, method, path, validBody string
		withDevice                    bool
	}{
		{"room create", "POST", "/api/controller/rooms", roomCreateBody, false},
		{"room update", "PUT", "/api/controller/rooms/room", roomUpdateBody, false},
		{"device create", "POST", "/api/controller/rooms/room/devices", deviceCreateBody, true},
		{"device update", "PUT", "/api/controller/rooms/room/devices/device", deviceUpdateBody, true},
	}
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			cases := []struct {
				name, body, contentType, wantError string
				wantStatus                         int
			}{
				{"JSON with parameters and trailing whitespace", write.validBody + " \n\t", "application/json; charset=utf-8", "", map[bool]int{true: 200, false: 201}[write.method == "PUT"]},
				{"exact 256 KiB accepted", write.validBody + strings.Repeat(" ", int(roomDeviceJSONBodyLimit)-len(write.validBody)), "application/json", "", map[bool]int{true: 200, false: 201}[write.method == "PUT"]},
				{"missing Content-Type", write.validBody, "", "Content-Type must be application/json", 415},
				{"wrong Content-Type", write.validBody, "text/plain", "Content-Type must be application/json", 415},
				{"malformed JSON", `{`, "application/json", "invalid request body", 400},
				{"empty body", "", "application/json", "invalid request body", 400},
				{"unknown field", strings.TrimSuffix(write.validBody, "}") + `,"extra":true}`, "application/json", "invalid request body", 400},
				{"second JSON value", write.validBody + `{}`, "application/json", "invalid request body", 400},
				{"trailing junk", write.validBody + `x`, "application/json", "invalid request body", 400},
				{"wrong field type", strings.Replace(write.validBody, `"name":"`, `"name":1,"unused":"`, 1), "application/json", "invalid request body", 400},
				{"over limit takes precedence", strings.Repeat("x", int(roomDeviceJSONBodyLimit)+1), "text/plain", "request body too large", 413},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					setRoomDeviceFixture(t, write.withDevice)
					recorder := roomDeviceRequest(t, write.method, write.path, test.body, test.contentType)
					if recorder.Code != test.wantStatus {
						t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
					}
					if test.wantError != "" {
						var body struct {
							OK    bool   `json:"ok"`
							Error string `json:"error"`
						}
						if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
							t.Fatalf("decode error body: %v", err)
						}
						if body.OK || body.Error != test.wantError {
							t.Fatalf("error body = %#v, want %q", body, test.wantError)
						}
						switch write.name {
						case "room create":
							if _, err := roomService.GetRoom("newroom"); err == nil {
								t.Fatal("invalid create persisted Room")
							}
						case "room update":
							room, err := roomService.GetRoom("room")
							if err != nil || room.Name != "Fixture Room" {
								t.Fatalf("invalid update changed Room: %#v, %v", room, err)
							}
						case "device create":
							if _, err := deviceService.GetDevice("room", "newdevice"); err == nil {
								t.Fatal("invalid create persisted Device")
							}
						case "device update":
							device, err := deviceService.GetDevice("room", "device")
							if err != nil || device.Name != "Fixture Device" {
								t.Fatalf("invalid update changed Device: %#v, %v", device, err)
							}
						}
					}
				})
			}
		})
	}
}
