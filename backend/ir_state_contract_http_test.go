package main

import (
	"backend/models"
	"backend/repositories"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfiguredStateConditionalAPIContract(t *testing.T) {
	raw, err := os.ReadFile("services/testdata/ir_state_schema_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]struct {
		Schema models.IRStateSchema
		States []struct {
			Value map[string]interface{}
			Valid bool
		}
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			f := newMaintenanceReadbackFixture(t, name+"_state_contract")
			rooms := filepath.Join(f.config.DataRoot, "rooms")
			if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "room", Name: "Room"}); err != nil {
				t.Fatal(err)
			}
			if err := repositories.NewDeviceRepositoryWithRoomsDir(rooms).Save(models.Device{ID: "device", Name: "Device", RoomID: "room", DeviceType: "other", PrimaryAdapter: "ir"}); err != nil {
				t.Fatal(err)
			}
			repo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
			if err := repo.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
				t.Fatal(err)
			}
			if err := repo.SaveStateSchema("room", "device", fixture.Schema); err != nil {
				t.Fatal(err)
			}
			mux := newHTTPMuxWithConfig(f.config)
			base := "/api/controller/rooms/room/devices/device/ir/states"
			request := func(method, path, body string, status int) []byte {
				t.Helper()
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != status {
					t.Fatalf("%s %s: %d want=%d %s", method, path, w.Code, status, w.Body.String())
				}
				return w.Body.Bytes()
			}
			for i, tc := range fixture.States {
				id := fmt.Sprintf("state_%d", i)
				body, _ := json.Marshal(map[string]interface{}{"id": id, "name": id, "state": tc.Value, "format": "raw_samples"})
				if !tc.Valid {
					request(http.MethodPost, base, string(body), 400)
					if exists, err := repo.StateExists("room", "device", id); err != nil || exists {
						t.Fatal("invalid create saved a file")
					}
					continue
				}
				request(http.MethodPost, base, string(body), 201)
				update, _ := json.Marshal(map[string]interface{}{"state": tc.Value})
				request(http.MethodPut, base+"/"+id, string(update), 200)
				var got models.IRState
				if err := json.Unmarshal(request(http.MethodGet, base+"/"+id, "", 200), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.State, tc.Value) {
					t.Fatal("State readback lost values")
				}
				before, _ := json.Marshal(got)
				request(http.MethodPut, base+"/"+id, `{"state":null}`, 400)
				request(http.MethodPut, base+"/"+id, `{"state":{"extra":true}}`, 400)
				after, err := repo.LoadState("room", "device", id)
				if err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(after)
				if string(encoded) != string(before) {
					t.Fatal("rejection changed state")
				}
			}
			// Seed an incompatible learned record to test operation HTTP status/shapes.
			learned := models.IRState{ID: "invalid_saved", Name: "Invalid", State: map[string]interface{}{"extra": true}, Format: models.OperationFormatRawSamples, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}
			if err := repo.SaveState("room", "device", learned); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "states", learned.ID+".json")
			before, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"capture", "send"} {
				response := request(http.MethodPost, base+"/"+learned.ID+"/"+action, `{}`, 409)
				var payload models.IRSendErrorResponse
				if err := json.Unmarshal(response, &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Issues) == 0 {
					t.Fatal("missing field reasons")
				}
				if action == "send" && (payload.DeliveryState != models.IRDeliveryStateNotSent || payload.CompletedRepeats != 0 || len(payload.RetryableWith) != 0) {
					t.Fatalf("invalid send response: %s", response)
				}
			}
			after, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("operation rejection changed bytes")
			}
		})
	}
}
func TestConfiguredStateEmptyObjectAndRegisteredEditHTTP(t *testing.T) {
	f := newMaintenanceReadbackFixture(t, "optional_state")
	rooms := filepath.Join(f.config.DataRoot, "rooms")
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "room", Name: "Room"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.NewDeviceRepositoryWithRoomsDir(rooms).Save(models.Device{ID: "device", Name: "Device", RoomID: "room", DeviceType: "other", PrimaryAdapter: "ir"}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
	if err := repo.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatal(err)
	}
	var schema models.IRStateSchema
	if err := json.Unmarshal([]byte(`{"note":{"type":"string","presence":"optional"},"eco":{"type":"boolean","presence":"optional"},"n":{"type":"number","presence":"optional"}}`), &schema); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveStateSchema("room", "device", schema); err != nil {
		t.Fatal(err)
	}
	mux := newHTTPMuxWithConfig(f.config)
	base := "/api/controller/rooms/room/devices/device/ir/states"
	request := func(method, path, body string, status int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d want=%d %s", method, w.Code, status, w.Body.String())
		}
	}
	request("POST", base, `{"id":"empty","name":"Empty","state":{},"format":"raw_samples"}`, 201)
	request("PUT", base+"/empty", `{"state":{"note":"","eco":false,"n":0}}`, 200)
	saved, err := repo.LoadState("room", "device", "empty")
	if err != nil {
		t.Fatal(err)
	}
	if saved.State["note"] != "" || saved.State["eco"] != false || saved.State["n"] != float64(0) {
		t.Fatal("empty/false/zero lost")
	}
	saved.Count = 4
	saved.TimingsUS = []uint{100, 200, 300, 400}
	if err := repo.SaveState("room", "device", saved); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "states", "empty.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	request("PUT", base+"/empty", `{"state":{}}`, 409)
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("learned edit changed bytes")
	}
	request("PUT", base+"/empty", `{"name":"Renamed","repeat":2,"state":{"n":0,"eco":false,"note":""}}`, 200)
	// Explicit empty object is valid for an unlearned record; omission/null are not equivalent.
	request("POST", base, `{"id":"missing","name":"Missing","format":"raw_samples"}`, 400)
	request("POST", base, `{"id":"null_state","name":"Null","state":null,"format":"raw_samples"}`, 400)
}
