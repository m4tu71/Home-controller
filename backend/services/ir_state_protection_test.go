package services

import (
	"backend/controller_client"
	"backend/models"
	"backend/repositories"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func protectedStateService(t *testing.T) (IRAdapterService, func(string) []byte) {
	t.Helper()
	s := newStateTestService(t, models.IRRemoteTypeStateful, true)
	rooms := filepath.Join(t.TempDir(), "rooms")
	s.irAdapterRepository = repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
	if err := s.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatal(err)
	}
	read := func(id string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", "states", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return s, read
}
func TestStateRegisteredSignalEditsPreserveBytes(t *testing.T) {
	for _, format := range []models.OperationFormat{models.OperationFormatRawSamples, models.OperationFormatRaw, models.OperationFormatCode, models.OperationFormatPresetCommand} {
		t.Run(string(format), func(t *testing.T) {
			s, read := protectedStateService(t)
			if _, err := s.SaveStateSchema("room", "device", schemaFromJSON(t, `{"power":{"type":"enum","values":["on","off"]},"eco":{"type":"boolean","presence":"optional"}}`)); err != nil {
				t.Fatal(err)
			}
			original := models.IRState{ID: "registered", Name: "Registered", State: map[string]interface{}{"power": "on", "eco": false}, Format: format, CarrierKHz: 38}
			switch format {
			case models.OperationFormatRawSamples, models.OperationFormatRaw:
				original.Count = 4
				original.TimingsUS = []uint{100, 200, 300, 400}
			case models.OperationFormatCode:
				original.Protocol = "PANASONIC_40"
				original.Code = "344A90BC2C"
			case models.OperationFormatPresetCommand:
				original.PicoCommand = "LIGHT_ON_CH3"
			}
			if err := s.irAdapterRepository.SaveState("room", "device", original); err != nil {
				t.Fatal(err)
			}
			before := read(original.ID)
			requests := []models.IRStateWriteRequest{
				{State: statePointer(map[string]interface{}{"power": "off", "eco": false})},
				{State: statePointer(map[string]interface{}{"power": "on"})},
				{Format: operationFormatPointer(models.OperationFormatRawSamples)},
				{CarrierKHz: intPointerForStateTest(56)},
			}
			if format == models.OperationFormatRawSamples {
				requests[2].Format = operationFormatPointer(models.OperationFormatRaw)
			}
			switch format {
			case models.OperationFormatRaw:
				timings := []uint{110, 210, 310, 410}
				requests = append(requests, models.IRStateWriteRequest{TimingsUS: &timings})
			case models.OperationFormatCode:
				requests = append(requests, models.IRStateWriteRequest{Code: stringPointer("0000000000")})
			case models.OperationFormatPresetCommand:
				requests = append(requests, models.IRStateWriteRequest{PicoCommand: stringPointer("LIGHT_OFF_CH3")})
			}
			for _, request := range requests {
				_, err := s.UpdateState("room", "device", original.ID, request)
				assertServiceErrorKind(t, err, ErrorConflict)
				if string(read(original.ID)) != string(before) {
					t.Fatal("rejection modified registered bytes")
				}
			}
			name := "Renamed"
			repeat := 2
			updated, err := s.UpdateState("room", "device", original.ID, models.IRStateWriteRequest{Name: &name, Repeat: &repeat, State: statePointer(map[string]interface{}{"eco": false, "power": "on"})})
			if err != nil {
				t.Fatal(err)
			}
			original.Name = name
			original.Repeat = repeat
			if !reflect.DeepEqual(original, updated) {
				t.Fatal("metadata update changed signal or state")
			}
		})
	}
}
func intPointerForStateTest(value int) *int { return &value }

func TestStateSharedFixturesBlockInvalidOperationsAndPreserveLearning(t *testing.T) {
	raw, err := os.ReadFile("testdata/ir_state_schema_contract.json")
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
			s, read := protectedStateService(t)
			if _, err := s.SaveStateSchema("room", "device", fixture.Schema); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.capture = func(attempt int) models.IRCapture {
				calls++
				return models.IRCapture{Attempt: attempt, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}
			}
			s.sendRawResult = func([]uint) (controller_client.CommandResult, error) {
				calls++
				return controller_client.CommandResult{ResponseBody: "OK"}, nil
			}
			s.sendPresetResult = func(string) (controller_client.CommandResult, error) {
				calls++
				return controller_client.CommandResult{ResponseBody: "OK"}, nil
			}
			s.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
				calls++
				return models.IRCapabilities{}, controller_client.CommandResult{}, nil
			}
			for _, tc := range fixture.States {
				state := models.IRState{ID: "sample", Name: "Sample", State: tc.Value, Format: models.OperationFormatRawSamples}
				if err := s.irAdapterRepository.SaveState("room", "device", state); err != nil {
					t.Fatal(err)
				}
				before := read(state.ID)
				calls = 0
				if !tc.Valid {
					_, err := s.CaptureState("room", "device", state.ID, models.IRStateCaptureRequest{})
					assertServiceErrorKind(t, err, ErrorConflict)
					_, err = s.SendState("room", "device", state.ID, models.IRStateSendRequest{})
					assertServiceErrorKind(t, err, ErrorConflict)
					_, err = s.PrepareStateSendPlan("room", "device", state.ID)
					assertServiceErrorKind(t, err, ErrorConflict)
					if calls != 0 || string(read(state.ID)) != string(before) {
						t.Fatal("invalid State reached transport or changed bytes")
					}
					continue
				}
				samples := 1
				response, err := s.CaptureState("room", "device", state.ID, models.IRStateCaptureRequest{Samples: &samples})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(response.State.State, tc.Value) {
					t.Fatal("capture changed values")
				}
				send, err := s.SendState("room", "device", state.ID, models.IRStateSendRequest{})
				if err != nil || send.DeliveryState != models.IRDeliveryStateSent {
					t.Fatalf("send: %#v %v", send, err)
				}
				learned := read(state.ID)
				s.capture = func(attempt int) models.IRCapture {
					return models.IRCapture{Attempt: attempt, Valid: false, Error: "cancelled fixture"}
				}
				attempts := 1
				_, err = s.CaptureState("room", "device", state.ID, models.IRStateCaptureRequest{Samples: &samples, MaxAttempts: &attempts})
				assertServiceErrorKind(t, err, ErrorCaptureInsufficient)
				if string(read(state.ID)) != string(learned) {
					t.Fatal("failed recapture changed learned bytes")
				}
				s.capture = func(attempt int) models.IRCapture {
					calls++
					return models.IRCapture{Attempt: attempt, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}
				}
			}
		})
	}
}
