package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/controller_client"
	"backend/handlers"
	"backend/models"
)

// All appliance data and the simulated daemon socket belong to temporary roots.
// The waveform is synthetic and does not describe any actual AC protocol.
func TestConfiguredStatefulDailyOperationAndReadback(t *testing.T) {
	fixture := newMaintenanceReadbackFixture(t, "step4_stateful")
	fixture.config.SocketPath = filepath.Join(t.TempDir(), "controller.sock")
	listener, err := net.Listen("unix", fixture.config.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Cleanup(func() {
		_ = controller_client.ConfigureSocketPath("/run/web-controller/controller-daemon.sock")
		handlers.ConfigureDataRoot("")
	})

	exchanges := []struct{ command, response string }{
		{"IR_CAPTURE", `{"ok":true,"timings_us":[9000,4500,550,550,550,550,550,550,550,550]}`},
		{"IR_CAPTURE", `{"ok":true,"timings_us":[9000,4500,560,560,560,560,560,560,560,560]}`},
		{"IR_CAPTURE", `{"ok":true,"timings_us":[9000,4500,570,570,570,570,570,570,570,570]}`},
		{"IR_SEND_RAW:9000,4500,560,560,560,560,560,560,560,560", `{"ok":true}`},
		{"IR_CAPTURE", `{"ok":false,"error":"timeout"}`},
	}
	daemonResult := make(chan error, 1)
	go func() {
		for _, exchange := range exchanges {
			connection, err := listener.Accept()
			if err != nil {
				daemonResult <- err
				return
			}
			_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
			command, err := bufio.NewReader(connection).ReadString('\n')
			if err == nil && strings.TrimSpace(command) != exchange.command {
				err = fmt.Errorf("daemon command=%q, want %q", command, exchange.command)
			}
			if err == nil {
				_, err = fmt.Fprintln(connection, exchange.response)
			}
			_ = connection.Close()
			if err != nil {
				daemonResult <- err
				return
			}
		}
		daemonResult <- nil
	}()

	mux := newHTTPMuxWithConfig(fixture.config)
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, r)
		if response.Code != status {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, status, response.Body.String())
		}
		return response.Body.Bytes()
	}
	roomPath := "/api/controller/rooms/bedroom"
	devicePath := roomPath + "/devices/bedroom_ac"
	irPath := devicePath + "/ir"
	statePath := irPath + "/states/cool_26_auto"
	request(http.MethodPost, "/api/controller/rooms", `{"id":"bedroom","name":"寝室"}`, http.StatusCreated)
	request(http.MethodPost, roomPath+"/devices", `{"id":"bedroom_ac","name":"寝室エアコン","room_id":"bedroom","device_type":"air_conditioner","primary_adapter":"ir"}`, http.StatusCreated)
	request(http.MethodPut, irPath+"/config", `{"ir_remote_type":"stateful","carrier_khz":38,"default_repeat":1}`, http.StatusOK)
	schema := `{"power":{"type":"enum","values":["on","off"]},"mode":{"type":"enum","values":["cool","heat"]},"target_temperature":{"type":"number","min":16,"max":30},"fan":{"type":"enum","values":["auto","low"]},"swing":{"type":"enum","values":["auto","fixed"]}}`
	request(http.MethodPut, irPath+"/state-schema", schema, http.StatusOK)
	state := `{"id":"cool_26_auto","name":"冷房26℃自動","state":{"power":"on","mode":"cool","target_temperature":26,"fan":"auto","swing":"auto"},"format":"raw_samples"}`
	request(http.MethodPost, irPath+"/states", state, http.StatusCreated)
	request(http.MethodPost, statePath+"/send", `{}`, http.StatusConflict)
	request(http.MethodPost, statePath+"/capture", `{"samples":3,"max_attempts":3}`, http.StatusOK)
	learned := request(http.MethodGet, statePath, "", http.StatusOK)
	var saved models.IRState
	if err := json.Unmarshal(learned, &saved); err != nil || saved.Count != 10 || len(saved.Captures) != 3 || saved.State["target_temperature"] != float64(26) {
		t.Fatalf("learned state=%+v err=%v", saved, err)
	}

	// Rebuild all mux dependencies to test disk readback without a process restart.
	paths := []string{roomPath, devicePath, irPath + "/config", irPath + "/state-schema", statePath, irPath + "/states"}
	before := make(map[string][]byte)
	for _, path := range paths {
		before[path] = request(http.MethodGet, path, "", http.StatusOK)
	}
	mux = newHTTPMuxWithConfig(fixture.config)
	for _, path := range paths {
		if got := request(http.MethodGet, path, "", http.StatusOK); !bytes.Equal(got, before[path]) {
			t.Fatalf("readback changed after mux reconstruction: %s\nbefore=%s\nafter=%s", path, before[path], got)
		}
	}
	var sent models.IRStateSendResponse
	if err := json.Unmarshal(request(http.MethodPost, statePath+"/send", `{"repeat":1}`, http.StatusOK), &sent); err != nil || !sent.OK || sent.DeliveryState != models.IRDeliveryStateSent || sent.CompletedRepeats != 1 || sent.RequestedRepeats != 1 {
		t.Fatalf("send response=%+v err=%v", sent, err)
	}
	request(http.MethodPost, statePath+"/capture", `{"samples":1,"max_attempts":1}`, http.StatusUnprocessableEntity)
	if got := request(http.MethodGet, statePath, "", http.StatusOK); !bytes.Equal(got, learned) {
		t.Fatalf("failed re-learning changed stored State: %s", got)
	}
	if err := <-daemonResult; err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	var failed models.IRSendErrorResponse
	if err := json.Unmarshal(request(http.MethodPost, statePath+"/send", `{"repeat":1}`, http.StatusInternalServerError), &failed); err != nil || failed.OK || failed.DeliveryState != models.IRDeliveryStateNotSent || failed.CompletedRepeats != 0 {
		t.Fatalf("unavailable daemon response=%+v err=%v", failed, err)
	}
	if got := request(http.MethodGet, statePath, "", http.StatusOK); !bytes.Equal(got, learned) {
		t.Fatalf("sending changed stored State: %s", got)
	}
}
