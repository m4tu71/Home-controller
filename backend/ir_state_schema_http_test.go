package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

func TestConfiguredSchemaHTTPRoundTripAndRejectedWritesPreserveFiles(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "schema_contract")
	rooms := filepath.Join(fixture.config.DataRoot, "rooms")
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "schema_room", Name: "Schema Room"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.NewDeviceRepositoryWithRoomsDir(rooms).Save(models.Device{ID: "schema_device", Name: "Schema Device", RoomID: "schema_room", DeviceType: "air_conditioner", PrimaryAdapter: "ir"}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
	if err := repo.SaveConfig("schema_room", "schema_device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatal(err)
	}
	path := "/api/controller/rooms/schema_room/devices/schema_device/ir/state-schema"
	mux := newHTTPMuxWithConfig(fixture.config)
	request := func(method, body string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, r)
		if response.Code != want {
			t.Fatalf("%s: status=%d want=%d body=%s", method, response.Code, want, response.Body.String())
		}
		return response.Body.Bytes()
	}
	equivalent := func(a, b []byte) {
		t.Helper()
		var x, y interface{}
		if err := json.Unmarshal(a, &x); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &y); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(x, y) {
			t.Fatalf("round trip: %s != %s", a, b)
		}
	}
	root := filepath.Join(rooms, "schema_room", "devices", "schema_device", "adapters", "ir")
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	legacy := `{"note":{"type":"string","min":0},"n":{"type":"number","values":["unused"]},"choice":{"type":"enum","values":[""]}}`
	// Directly saved old-format fixture, rather than only a new service write.
	if err := os.WriteFile(filepath.Join(root, "state_schema.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	equivalent([]byte(legacy), request(http.MethodGet, "", 200))
	equivalent([]byte(legacy), request(http.MethodPut, legacy, 200))
	state := models.IRState{ID: "old_record", Name: "Old Record", State: map[string]interface{}{"note": "abc", "n": float64(1), "choice": ""}, Format: models.OperationFormatRawSamples, Count: 2, TimingsUS: []uint{9000, 4500}, Captures: []models.IRCapture{{Count: 2, TimingsUS: []uint{9000, 4500}}}}
	if err := repo.SaveState("schema_room", "schema_device", state); err != nil {
		t.Fatal(err)
	}
	// Display-only edit must keep the legacy field's unrelated min.
	displayed := strings.Replace(legacy, `"min":0`, `"min":0,"label":"Note"`, 1)
	equivalent([]byte(displayed), request(http.MethodPut, displayed, 200))
	beforeSchema, beforeState := read("state_schema.json"), read("states/old_record.json")
	unchanged := func() {
		t.Helper()
		if !bytes.Equal(beforeSchema, read("state_schema.json")) || !bytes.Equal(beforeState, read("states/old_record.json")) {
			t.Fatal("rejected write changed existing file bytes")
		}
	}
	for _, body := range []string{
		strings.Replace(displayed, `"min":0`, `"min":0,"presence":"required"`, 1),
		`{"choice":{"type":"enum","presence":"required","values":[null]}}`,
		`{"selector":{"type":"boolean"},"choice":{"type":"enum","values":[""],"cases":[{"when":{"all":[{"field":"selector","in":[true]}]},"presence":"required","values":[null]}]}}`,
		`{"n":{"type":"number","min":2,"max":1}}`,
		`{"n":{"type":"number","step":0}}`,
		`{"n":{"type":"number","presence":null}}`,
		`{"n":{"type":"number","Presence":null}}`,
		`{"n":{"type":"number","Step":null}}`,
		`{"n":{"type":"number","Cases":null}}`,
		`{"selector":{"type":"boolean"},"n":{"type":"number","cases":[{"when":{"all":[{"field":"selector","in":[true]}]},"presence":"required","Step":null}]}}`,
		`{"n":{"type":"number","extra":true}}`,
		`{"n":{"type":"number","cases":[{"when":{"all":[{"field":"missing","in":[true]}]},"presence":"required"}]}}`,
		`{"n":{"type":"number"}," n ":{"type":"number"}}`,
	} {
		t.Run(body, func(t *testing.T) { request(http.MethodPut, body, 400); unchanged() })
	}
	conflict := strings.Replace(displayed, `"n":{"type":"number","values":["unused"]}`, `"n":{"type":"number","presence":"absent"}`, 1)
	response := request(http.MethodPut, conflict, 409)
	var payload struct {
		InvalidStateIDs []string                    `json:"invalid_state_ids"`
		Issues          []models.IRStateSchemaIssue `json:"issues"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload.InvalidStateIDs, []string{"old_record"}) || len(payload.Issues) != 1 || payload.Issues[0].FieldID != "n" || payload.Issues[0].StateID != "old_record" || payload.Issues[0].Code != "absent" {
		t.Fatalf("conflict reasons: %s", response)
	}
	unchanged()
	// Explicit removal of the irrelevant legacy min permits extension.
	extended := strings.Replace(displayed, `"min":0,`, `"presence":"required",`, 1)
	equivalent([]byte(extended), request(http.MethodPut, extended, 200))
	if !bytes.Equal(beforeState, read("states/old_record.json")) {
		t.Fatal("extension changed learned State")
	}
	// New mux/repositories read back the saved contract after reconstruction.
	mux = newHTTPMuxWithConfig(fixture.config)
	equivalent([]byte(extended), request(http.MethodGet, "", 200))
	// Corrupt existing records must not be skipped during compatibility checks.
	if err := os.WriteFile(filepath.Join(root, "states", "broken.json"), []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	beforeSchema = read("state_schema.json")
	request(http.MethodPut, extended, 500)
	unchanged()
	if !bytes.Equal(read("states/broken.json"), []byte(`{`)) {
		t.Fatal("corrupt record changed")
	}
}

func TestConfiguredExtendedSchemaHTTPReadback(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "extended_schema")
	rooms := filepath.Join(fixture.config.DataRoot, "rooms")
	if err := repositories.NewRoomRepositoryWithBaseDir(rooms).Save(models.Room{ID: "schema_room", Name: "Schema Room"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.NewDeviceRepositoryWithRoomsDir(rooms).Save(models.Device{ID: "schema_device", Name: "Schema Device", RoomID: "schema_room", DeviceType: "other", PrimaryAdapter: "ir"}); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
	if err := repo.SaveConfig("schema_room", "schema_device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatal(err)
	}
	schema := `{"enabled":{"type":"boolean"},"profile":{"type":"enum","values":["normal","boost"]},"level":{"type":"number","presence":"absent","label":"Level","unit":"","cases":[{"when":{"all":[{"field":"enabled","in":[false]}]},"presence":"optional","min":-2,"max":2,"step":0.5,"description":"Test"}]},"note":{"type":"string","presence":"optional","allow_empty":false,"cases":[]}}`
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		mux := newHTTPMuxWithConfig(fixture.config)
		request := httptest.NewRequest(method, "/api/controller/rooms/schema_room/devices/schema_device/ir/state-schema", strings.NewReader(schema))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", method, response.Code, response.Body.String())
		}
		var want, got interface{}
		if err := json.Unmarshal([]byte(schema), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s: schema changed: %s", method, response.Body.String())
		}
	}
}
