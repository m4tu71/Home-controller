package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"backend/controller_client"
	backendir "backend/ir"
	"backend/models"
	"backend/repositories"
)

func TestIRCommandWriteRequestDistinguishesOmittedLearningFields(t *testing.T) {
	var request models.IRCommandWriteRequest
	if err := json.Unmarshal([]byte(`{"id":"power","name":"Power","format":"raw_samples","count":0}`), &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if request.Count == nil || *request.Count != 0 {
		t.Fatal("count must retain an explicit zero value")
	}
	if request.TimingsUS != nil || request.Captures != nil {
		t.Fatal("omitted learning fields must remain nil")
	}
}

func TestIRStateWriteRequestDistinguishesOmittedLearningFields(t *testing.T) {
	var request models.IRStateWriteRequest
	if err := json.Unmarshal([]byte(`{"id":"cool_26_auto","name":"Cool 26 Auto","state":{"power":"on"},"format":"raw_samples","count":0}`), &request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if request.Count == nil || *request.Count != 0 {
		t.Fatal("count must retain an explicit zero value")
	}
	if request.TimingsUS != nil || request.Captures != nil {
		t.Fatal("omitted learning fields must remain nil")
	}
	if request.State == nil || (*request.State)["power"] != "on" {
		t.Fatal("state payload must be retained")
	}
}

func TestIRStateActionRequestsDistinguishOmittedAndZero(t *testing.T) {
	var captureRequest models.IRStateCaptureRequest
	if err := json.Unmarshal([]byte(`{"samples":0,"max_attempts":0}`), &captureRequest); err != nil {
		t.Fatalf("unmarshal capture request: %v", err)
	}
	if captureRequest.Samples == nil || *captureRequest.Samples != 0 || captureRequest.MaxAttempts == nil || *captureRequest.MaxAttempts != 0 {
		t.Fatal("capture request must retain explicit zero values")
	}

	var sendRequest models.IRStateSendRequest
	if err := json.Unmarshal([]byte(`{"repeat":0}`), &sendRequest); err != nil {
		t.Fatalf("unmarshal send request: %v", err)
	}
	if sendRequest.Repeat == nil || *sendRequest.Repeat != 0 {
		t.Fatal("send request must retain an explicit zero value")
	}
}

func TestIRExecutionContractModels(t *testing.T) {
	mode := models.IRExecutionModeBackendRaw
	var request models.IRCommandSendRequest
	if err := json.Unmarshal([]byte(`{"repeat":1,"execution_mode":"backend_raw"}`), &request); err != nil {
		t.Fatalf("unmarshal send request: %v", err)
	}
	if request.Repeat == nil || *request.Repeat != 1 || request.ExecutionMode == nil || *request.ExecutionMode != mode {
		t.Fatalf("send request = %#v", request)
	}

	options := models.IRExecutionOptionsResponse{
		Format:   models.OperationFormatCode,
		Protocol: "PANASONIC_40",
		ExecutionModes: models.IRExecutionModeOptions{
			NativeCode: models.IRExecutionModeOption{Available: false, VerificationStatus: models.IRVerificationStatusUnknown, Reason: "capability not confirmed"},
			BackendRaw: models.IRExecutionModeOption{Available: true, VerificationStatus: models.IRVerificationStatusConfirmed},
		},
	}
	payload, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("marshal execution options: %v", err)
	}
	for _, field := range []string{`"format":"code"`, `"protocol":"PANASONIC_40"`, `"native_code"`, `"backend_raw"`, `"verification_status":"unknown"`} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("execution options JSON %s does not contain %s", payload, field)
		}
	}
}

func TestGetExecutionOptionsReportsBackendAndUnconfirmedNativeCapabilities(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"})

	options, err := service.GetExecutionOptions("room", "device", " PANASONIC_40 ")
	if err != nil {
		t.Fatalf("GetExecutionOptions() error = %v", err)
	}
	if options.Format != models.OperationFormatCode || options.Protocol != "PANASONIC_40" {
		t.Fatalf("execution options = %#v", options)
	}
	if !options.ExecutionModes.BackendRaw.Available || options.ExecutionModes.BackendRaw.VerificationStatus != models.IRVerificationStatusConfirmed || options.ExecutionModes.BackendRaw.Reason != "" {
		t.Fatalf("backend_raw option = %#v", options.ExecutionModes.BackendRaw)
	}
	if options.ExecutionModes.NativeCode.Available || options.ExecutionModes.NativeCode.VerificationStatus != models.IRVerificationStatusUnknown || options.ExecutionModes.NativeCode.Reason == "" {
		t.Fatalf("native_code option = %#v", options.ExecutionModes.NativeCode)
	}
}

func TestGetExecutionOptionsRejectsMissingAndUnsupportedProtocol(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"})
	for _, protocol := range []string{"", " ", "NEC", "panasonic_40"} {
		t.Run(protocol, func(t *testing.T) {
			_, err := service.GetExecutionOptions("room", "device", protocol)
			assertServiceErrorMessage(t, err, "ir code protocol is not supported")
			assertServiceErrorKind(t, err, ErrorInvalidRequest)
		})
	}
}

func TestExecutionOptionsDoNotAuthorizeSendWithoutRuntimeValidation(t *testing.T) {
	command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, CarrierKHz: 56, Protocol: "PANASONIC_40", Code: "344A90BC2C"}
	service := newCaptureTestService(t, command)
	options, err := service.GetExecutionOptions("room", "device", command.Protocol)
	if err != nil || !options.ExecutionModes.BackendRaw.Available {
		t.Fatalf("execution options = %#v, %v", options, err)
	}

	sendCalls := 0
	service.sendRaw = func([]uint) (string, error) {
		sendCalls++
		return `{"ok":true}`, nil
	}
	backendRaw := models.IRExecutionModeBackendRaw
	_, err = service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &backendRaw})
	assertServiceErrorMessage(t, err, "ir carrier is not supported for execution")
	if sendCalls != 0 {
		t.Fatalf("send called %d times after runtime validation failure", sendCalls)
	}

	options.ExecutionModes.NativeCode.Available = true
	nativeCode := models.IRExecutionModeNativeCode
	_, err = service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &nativeCode})
	assertServiceErrorMessage(t, err, "native code execution is not available")
	if sendCalls != 0 {
		t.Fatalf("locally modified options authorized %d sends", sendCalls)
	}
}

func TestIRWirePreSendRejectionAllowlist(t *testing.T) {
	allowed := []models.IRWireErrorCode{
		models.IRWireErrorCodeUnsupportedCommand,
		models.IRWireErrorCodeUnsupportedProtocol,
		models.IRWireErrorCodeInvalidCode,
		models.IRWireErrorCodeInvalidRawTiming,
		models.IRWireErrorCodeInvalidRawSeparator,
		models.IRWireErrorCodeEmptyRawTimings,
		models.IRWireErrorCodeRawTimingExceedsMaximum,
		models.IRWireErrorCodeRawDurationExceedsMaximum,
		models.IRWireErrorCodeTooManyRawTimings,
	}
	for _, code := range allowed {
		if !code.IsPreSendRejection() {
			t.Errorf("wire error code %q must be a pre-send rejection", code)
		}
	}
	for _, code := range []models.IRWireErrorCode{"", "internal_error", "hardware_error", "send_failed", "unknown_code"} {
		if code.IsPreSendRejection() {
			t.Errorf("wire error code %q must not be a pre-send rejection", code)
		}
	}
}

func TestValidateIRExecutionMode(t *testing.T) {
	backendRaw := models.IRExecutionModeBackendRaw
	nativeCode := models.IRExecutionModeNativeCode
	invalid := models.IRExecutionMode("automatic")

	for _, mode := range []*models.IRExecutionMode{&backendRaw, &nativeCode} {
		if err := validateIRExecutionMode(models.OperationFormatCode, mode, "PANASONIC_40"); err != nil {
			t.Fatalf("code execution mode %q must be valid: %v", *mode, err)
		}
	}
	assertServiceErrorMessage(t, validateIRExecutionMode(models.OperationFormatCode, nil, "PANASONIC_40"), "ir execution mode is required")
	assertServiceErrorMessage(t, validateIRExecutionMode(models.OperationFormatCode, &invalid, "PANASONIC_40"), "invalid ir execution mode")

	for _, format := range []models.OperationFormat{models.OperationFormatRawSamples, models.OperationFormatRaw, models.OperationFormatPresetCommand} {
		if err := validateIRExecutionMode(format, nil, ""); err != nil {
			t.Fatalf("format %q without execution mode must remain valid: %v", format, err)
		}
		err := validateIRExecutionMode(format, &backendRaw, "")
		assertServiceErrorMessage(t, err, "invalid ir execution mode")
		var serviceErr *ServiceError
		if !errors.As(err, &serviceErr) {
			t.Fatalf("error type = %T, want *ServiceError", err)
		}
		response, ok := serviceErr.Payload.(models.IRSendErrorResponse)
		if !ok || response.DeliveryState != models.IRDeliveryStateNotSent || response.ExecutionMode != backendRaw || response.RetryableWith == nil {
			t.Fatalf("send error payload = %#v", serviceErr.Payload)
		}
	}
}

func TestIRStateActionResponseContracts(t *testing.T) {
	captureResponse := models.IRStateCaptureResponse{
		OK:              true,
		State:           models.IRState{ID: "cool_26_auto", Format: models.OperationFormatRawSamples},
		RequiredSamples: 3,
		ValidCaptures:   3,
	}
	captureJSON, err := json.Marshal(captureResponse)
	if err != nil {
		t.Fatalf("marshal capture response: %v", err)
	}
	var capturePayload struct {
		OK              bool           `json:"ok"`
		State           models.IRState `json:"state"`
		RequiredSamples int            `json:"required_samples"`
		ValidCaptures   int            `json:"valid_captures"`
	}
	if err := json.Unmarshal(captureJSON, &capturePayload); err != nil {
		t.Fatalf("unmarshal capture response: %v", err)
	}
	if !capturePayload.OK || capturePayload.State.ID != "cool_26_auto" || capturePayload.RequiredSamples != 3 || capturePayload.ValidCaptures != 3 {
		t.Fatalf("capture JSON payload = %#v", capturePayload)
	}

	insufficientResponse := models.IRStateCaptureInsufficientResponse{
		OK:                false,
		Error:             string(models.IRStateActionErrorNotEnoughCaptures),
		RequiredSamples:   3,
		ValidCaptures:     1,
		CountDistribution: map[string]int{"583": 1, "5": 2},
		Captures: []models.IRCapture{{
			Attempt:    1,
			OK:         true,
			Valid:      true,
			Count:      583,
			TimingsUS:  []uint{340, 500, 370},
			CapturedAt: time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		}},
	}
	insufficientJSON, err := json.Marshal(insufficientResponse)
	if err != nil {
		t.Fatalf("marshal insufficient response: %v", err)
	}
	var insufficientPayload struct {
		OK                bool               `json:"ok"`
		Error             string             `json:"error"`
		RequiredSamples   int                `json:"required_samples"`
		ValidCaptures     int                `json:"valid_captures"`
		CountDistribution map[string]int     `json:"count_distribution"`
		Captures          []models.IRCapture `json:"captures"`
	}
	if err := json.Unmarshal(insufficientJSON, &insufficientPayload); err != nil {
		t.Fatalf("unmarshal insufficient response: %v", err)
	}
	if insufficientPayload.OK || insufficientPayload.Error != "not enough valid captures" || insufficientPayload.RequiredSamples != 3 || insufficientPayload.ValidCaptures != 1 || insufficientPayload.CountDistribution["5"] != 2 || len(insufficientPayload.Captures) != 1 || insufficientPayload.Captures[0].Count != 583 {
		t.Fatalf("insufficient JSON payload = %#v", insufficientPayload)
	}

	sendResponse := models.IRStateSendResponse{
		OK:               true,
		RoomID:           "bedroom",
		DeviceID:         "bedroom_ac",
		StateID:          "cool_26_auto",
		Format:           models.OperationFormatRawSamples,
		DeliveryState:    models.IRDeliveryStateSent,
		Repeat:           1,
		RequestedRepeats: 1,
		CompletedRepeats: 1,
		RetryableWith:    []models.IRExecutionMode{},
	}
	sendJSON, err := json.Marshal(sendResponse)
	if err != nil {
		t.Fatalf("marshal send response: %v", err)
	}
	var sendPayload struct {
		OK               bool                     `json:"ok"`
		RoomID           string                   `json:"room_id"`
		DeviceID         string                   `json:"device_id"`
		StateID          string                   `json:"state_id"`
		Format           models.OperationFormat   `json:"format"`
		DeliveryState    models.IRDeliveryState   `json:"delivery_state"`
		Repeat           int                      `json:"repeat"`
		RequestedRepeats int                      `json:"requested_repeats"`
		CompletedRepeats int                      `json:"completed_repeats"`
		RetryableWith    []models.IRExecutionMode `json:"retryable_with"`
	}
	if err := json.Unmarshal(sendJSON, &sendPayload); err != nil {
		t.Fatalf("unmarshal send response: %v", err)
	}
	if !sendPayload.OK || sendPayload.RoomID != "bedroom" || sendPayload.DeviceID != "bedroom_ac" || sendPayload.StateID != "cool_26_auto" || sendPayload.Format != models.OperationFormatRawSamples || sendPayload.DeliveryState != models.IRDeliveryStateSent || sendPayload.Repeat != 1 || sendPayload.RequestedRepeats != 1 || sendPayload.CompletedRepeats != 1 || sendPayload.RetryableWith == nil {
		t.Fatalf("send JSON payload = %#v", sendPayload)
	}
}

func TestValidateRawSamplesCreateRequest(t *testing.T) {
	zero := 0
	if err := validateRawSamplesCreateRequest(models.IRCommandWriteRequest{Count: &zero, TimingsUS: &[]uint{}, Captures: &[]models.IRCapture{}}); err != nil {
		t.Fatalf("empty learning data must be allowed on create: %v", err)
	}

	count := 1
	err := validateRawSamplesCreateRequest(models.IRCommandWriteRequest{Count: &count})
	assertServiceErrorMessage(t, err, "ir command learning data is read only")
}

func TestMergeIRCommandWriteRequestPreservesRawSamplesLearningData(t *testing.T) {
	existing := models.IRCommand{
		ID:        "power",
		Name:      "Power",
		Format:    models.OperationFormatRawSamples,
		Count:     3,
		TimingsUS: []uint{100, 200, 300},
		Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 3}},
	}
	name := "Main power"
	command, err := mergeIRCommandWriteRequest(existing, models.IRCommandWriteRequest{Name: &name})
	if err != nil {
		t.Fatalf("merge request: %v", err)
	}
	if command.Name != name || command.Count != existing.Count || len(command.TimingsUS) != len(existing.TimingsUS) || len(command.Captures) != len(existing.Captures) {
		t.Fatal("metadata update must preserve learned raw_samples data")
	}

	emptyTimings := []uint{}
	err = func() error {
		_, err := mergeIRCommandWriteRequest(existing, models.IRCommandWriteRequest{TimingsUS: &emptyTimings})
		return err
	}()
	assertServiceErrorMessage(t, err, "ir command learning data is read only")
}

func TestMergeIRCommandWriteRequestRejectsLearnedFormatChange(t *testing.T) {
	existing := models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatRawSamples, Count: 3}
	format := models.OperationFormatPresetCommand
	_, err := mergeIRCommandWriteRequest(existing, models.IRCommandWriteRequest{Format: &format})
	assertServiceErrorMessage(t, err, "ir command learning data is read only")
}

func TestValidateCommandIRRemoteType(t *testing.T) {
	if err := validateCommandIRRemoteType(models.IRRemoteTypeCommand); err != nil {
		t.Fatalf("command type must be accepted: %v", err)
	}
	assertServiceErrorMessage(t, validateCommandIRRemoteType(models.IRRemoteTypeStateful), "ir remote type is not command")
}

func TestValidateStatefulIRRemoteType(t *testing.T) {
	if err := validateStatefulIRRemoteType(models.IRRemoteTypeStateful); err != nil {
		t.Fatalf("stateful type must be accepted: %v", err)
	}
	assertServiceErrorMessage(t, validateStatefulIRRemoteType(models.IRRemoteTypeCommand), "ir remote type is not stateful")
}

func TestStateOperationsRequireStatefulIRDevice(t *testing.T) {
	commandService := newStateTestService(t, models.IRRemoteTypeCommand, true)
	state := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	schema := models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}

	_, err := commandService.GetStateSchema("room", "device")
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.SaveStateSchema("room", "device", schema)
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.ListStates("room", "device")
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.GetState("room", "device", state.ID)
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.CreateState("room", "device", irStateWriteRequestFromState(state))
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.UpdateState("room", "device", state.ID, irStateWriteRequestFromState(state))
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
	_, err = commandService.DeleteState("room", "device", state.ID)
	assertServiceErrorMessage(t, err, "ir remote type is not stateful")
}

func TestStateOperationsRequireIRConfig(t *testing.T) {
	service := newStateTestService(t, "", false)
	_, err := service.ListStates("room", "device")
	assertServiceErrorMessage(t, err, "ir config not found")
	assertServiceErrorKind(t, err, ErrorNotFound)
}

func TestStatefulIRDeviceAllowsStateOperationsAndRejectsCommands(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	schema := models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}
	if _, err := service.SaveStateSchema("room", "device", schema); err != nil {
		t.Fatalf("save state schema: %v", err)
	}
	if _, err := service.GetStateSchema("room", "device"); err != nil {
		t.Fatalf("get state schema: %v", err)
	}

	state := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	if _, err := service.CreateState("room", "device", irStateWriteRequestFromState(state)); err != nil {
		t.Fatalf("create state: %v", err)
	}
	if _, err := service.ListStates("room", "device"); err != nil {
		t.Fatalf("list states: %v", err)
	}
	if _, err := service.GetState("room", "device", state.ID); err != nil {
		t.Fatalf("get state: %v", err)
	}
	state.Name = "Cool 27 Auto"
	if _, err := service.UpdateState("room", "device", state.ID, irStateWriteRequestFromState(state)); err != nil {
		t.Fatalf("update state: %v", err)
	}
	if _, err := service.DeleteState("room", "device", state.ID); err != nil {
		t.Fatalf("delete state: %v", err)
	}

	_, err := service.ListCommands("room", "device")
	assertServiceErrorMessage(t, err, "ir remote type is not command")
}

func TestStateWritesRequireSchemaWhileLegacyStateReadAndDeleteRemainAvailable(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	state := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatalf("save legacy state: %v", err)
	}

	_, err := service.CreateState("room", "device", irStateWriteRequestFromState(models.IRState{
		ID:     "heat_22_auto",
		Name:   "Heat 22 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}))
	assertServiceErrorMessage(t, err, "ir state schema not found")

	_, err = service.UpdateState("room", "device", state.ID, irStateWriteRequestFromState(state))
	assertServiceErrorMessage(t, err, "ir state schema not found")

	states, err := service.ListStates("room", "device")
	if err != nil || len(states) != 1 || states[0].ID != state.ID {
		t.Fatalf("legacy state list = %#v, %v", states, err)
	}
	if _, err := service.GetState("room", "device", state.ID); err != nil {
		t.Fatalf("get legacy state: %v", err)
	}
	if _, err := service.DeleteState("room", "device", state.ID); err != nil {
		t.Fatalf("delete legacy state: %v", err)
	}
}

func TestStateWritesStrictlyValidateSchema(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	min, max := 16.0, 30.0
	schema := models.IRStateSchema{
		"power":              {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
		"target_temperature": {Type: models.IRStateFieldTypeNumber, Min: &min, Max: &max},
		"eco":                {Type: models.IRStateFieldTypeBoolean},
		"label":              {Type: models.IRStateFieldTypeString},
	}
	if _, err := service.SaveStateSchema("room", "device", schema); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	validState := models.IRState{
		ID:   "cool_26_auto",
		Name: "Cool 26 Auto",
		State: map[string]interface{}{
			"power":              "on",
			"target_temperature": float64(26),
			"eco":                true,
			"label":              "night",
		},
		Format: models.OperationFormatRawSamples,
	}
	if _, err := service.CreateState("room", "device", irStateWriteRequestFromState(validState)); err != nil {
		t.Fatalf("create valid state: %v", err)
	}

	invalidStates := []models.IRState{
		{ID: "missing_field", Name: "Missing field", State: map[string]interface{}{"power": "on", "target_temperature": float64(26), "eco": true}, Format: models.OperationFormatRawSamples},
		{ID: "unknown_field", Name: "Unknown field", State: map[string]interface{}{"power": "on", "target_temperature": float64(26), "eco": true, "label": "night", "swing": "auto"}, Format: models.OperationFormatRawSamples},
		{ID: "invalid_enum", Name: "Invalid enum", State: map[string]interface{}{"power": "sleep", "target_temperature": float64(26), "eco": true, "label": "night"}, Format: models.OperationFormatRawSamples},
		{ID: "below_min", Name: "Below min", State: map[string]interface{}{"power": "on", "target_temperature": float64(15), "eco": true, "label": "night"}, Format: models.OperationFormatRawSamples},
		{ID: "above_max", Name: "Above max", State: map[string]interface{}{"power": "on", "target_temperature": float64(31), "eco": true, "label": "night"}, Format: models.OperationFormatRawSamples},
		{ID: "invalid_boolean", Name: "Invalid boolean", State: map[string]interface{}{"power": "on", "target_temperature": float64(26), "eco": "true", "label": "night"}, Format: models.OperationFormatRawSamples},
		{ID: "invalid_string", Name: "Invalid string", State: map[string]interface{}{"power": "on", "target_temperature": float64(26), "eco": true, "label": false}, Format: models.OperationFormatRawSamples},
	}
	for _, state := range invalidStates {
		_, err := service.CreateState("room", "device", irStateWriteRequestFromState(state))
		assertServiceErrorMessage(t, err, "ir state does not match schema")
	}

	invalidUpdate := validState
	invalidUpdate.State = map[string]interface{}{
		"power":              "on",
		"target_temperature": float64(26),
		"eco":                true,
		"label":              false,
	}
	_, err := service.UpdateState("room", "device", validState.ID, irStateWriteRequestFromState(invalidUpdate))
	assertServiceErrorMessage(t, err, "ir state does not match schema")
}

func TestStateSchemaUpdateRejectsConflictingExistingStates(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	legacyState := models.IRState{
		ID:     "legacy_state",
		Name:   "Legacy state",
		State:  map[string]interface{}{"power": "off"},
		Format: models.OperationFormatRawSamples,
	}
	if err := service.irAdapterRepository.SaveState("room", "device", legacyState); err != nil {
		t.Fatalf("save legacy state: %v", err)
	}

	_, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on"}},
	})
	assertServiceErrorMessage(t, err, "ir state schema conflicts with existing states")
	assertServiceErrorKind(t, err, ErrorConflict)

	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("error = %v, want service error", err)
	}
	payload, ok := serviceErr.Payload.(map[string]interface{})
	if !ok || payload["error"] != "ir state schema conflicts with existing states" || !reflect.DeepEqual(payload["invalid_state_ids"], []string{"legacy_state"}) {
		t.Fatalf("conflict payload = %#v", serviceErr.Payload)
	}

	_, err = service.GetStateSchema("room", "device")
	assertServiceErrorMessage(t, err, "ir state schema not found")
}

func TestStateWriteProtectsRawSamplesLearningData(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	learned := models.IRState{
		ID:        "cool_26_auto",
		Name:      "Cool 26 Auto",
		State:     map[string]interface{}{"power": "on"},
		Format:    models.OperationFormatRawSamples,
		Count:     3,
		TimingsUS: []uint{100, 200, 300},
		Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 3, TimingsUS: []uint{100, 200, 300}}},
	}
	if err := service.irAdapterRepository.SaveState("room", "device", learned); err != nil {
		t.Fatalf("save learned state: %v", err)
	}

	name := "Cool 26 Auto Updated"
	updated, err := service.UpdateState("room", "device", learned.ID, models.IRStateWriteRequest{Name: &name})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Name != name || updated.Count != learned.Count || !reflect.DeepEqual(updated.TimingsUS, learned.TimingsUS) || !reflect.DeepEqual(updated.Captures, learned.Captures) {
		t.Fatalf("updated learned state = %#v", updated)
	}

	zero := 0
	_, err = service.UpdateState("room", "device", learned.ID, models.IRStateWriteRequest{Count: &zero})
	assertServiceErrorMessage(t, err, "ir state learning data is read only")
	rawFormat := models.OperationFormatRaw
	_, err = service.UpdateState("room", "device", learned.ID, models.IRStateWriteRequest{Format: &rawFormat})
	assertServiceErrorKind(t, err, ErrorConflict)

	count := 1
	_, err = service.CreateState("room", "device", models.IRStateWriteRequest{
		ID:     stringPointer("new_raw_samples"),
		Name:   stringPointer("New raw samples"),
		State:  statePointer(map[string]interface{}{"power": "on"}),
		Format: operationFormatPointer(models.OperationFormatRawSamples),
		Count:  &count,
	})
	assertServiceErrorMessage(t, err, "ir state learning data is read only")
}

func TestStateWritePreservesNonRawSamplesFormatCompatibility(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	rawCount := 3
	rawTimings := []uint{100, 200, 300}
	raw, err := service.CreateState("room", "device", models.IRStateWriteRequest{
		ID:        stringPointer("raw_state"),
		Name:      stringPointer("Raw state"),
		State:     statePointer(map[string]interface{}{"power": "on"}),
		Format:    operationFormatPointer(models.OperationFormatRaw),
		Count:     &rawCount,
		TimingsUS: &rawTimings,
	})
	if err != nil {
		t.Fatalf("create raw state: %v", err)
	}
	rawCount = 4
	rawTimings = []uint{110, 210, 310, 410}
	_, err = service.UpdateState("room", "device", raw.ID, models.IRStateWriteRequest{Count: &rawCount, TimingsUS: &rawTimings})
	assertServiceErrorKind(t, err, ErrorConflict)
	rawSamplesFormat := models.OperationFormatRawSamples
	_, err = service.UpdateState("room", "device", raw.ID, models.IRStateWriteRequest{Format: &rawSamplesFormat})
	assertServiceErrorKind(t, err, ErrorConflict)
	saved, err := service.irAdapterRepository.LoadState("room", "device", raw.ID)
	if err != nil || !reflect.DeepEqual(raw, saved) {
		t.Fatalf("rejected edit changed raw State: %v", err)
	}

	for _, state := range []models.IRState{
		{ID: "code_state", Name: "Code state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"},
		{ID: "preset_state", Name: "Preset state", State: map[string]interface{}{"power": "off"}, Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_OFF_CH3"},
	} {
		created, err := service.CreateState("room", "device", irStateWriteRequestFromState(state))
		if err != nil {
			t.Fatalf("create %s: %v", state.Format, err)
		}
		name := created.Name + " updated"
		updated, err := service.UpdateState("room", "device", created.ID, models.IRStateWriteRequest{Name: &name})
		if err != nil || updated.Name != name || updated.Protocol != created.Protocol || updated.Code != created.Code || updated.PicoCommand != created.PicoCommand {
			t.Fatalf("update %s = %#v, %v", state.Format, updated, err)
		}
	}
}

func TestRawOperationValidationIsSharedByCommandAndState(t *testing.T) {
	validCounts := []int{backendir.MaxRawTimings - 1, backendir.MaxRawTimings}
	for _, count := range validCounts {
		timings := make([]uint, count)
		for i := range timings {
			timings[i] = 1
		}
		command := models.IRCommand{ID: "raw_command", Name: "Raw command", Format: models.OperationFormatRaw, CarrierKHz: 56, Count: count, TimingsUS: timings}
		state := models.IRState{ID: "raw_state", Name: "Raw state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: 56, Count: count, TimingsUS: timings}
		if err := validateIRCommand(command); err != nil {
			t.Fatalf("Command count %d must be valid: %v", count, err)
		}
		if err := validateIRState(state); err != nil {
			t.Fatalf("State count %d must be valid: %v", count, err)
		}
	}

	validSafetyBoundaries := []struct {
		name    string
		timings []uint
	}{
		{name: "single timing immediately below maximum", timings: []uint{backendir.MaxRawTimingUS - 1}},
		{name: "duration immediately below maximum", timings: []uint{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 99999}},
	}
	for _, test := range validSafetyBoundaries {
		t.Run(test.name, func(t *testing.T) {
			command := models.IRCommand{ID: "raw_command", Name: "Raw command", Format: models.OperationFormatRaw, Count: len(test.timings), TimingsUS: test.timings}
			state := models.IRState{ID: "raw_state", Name: "Raw state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, Count: len(test.timings), TimingsUS: test.timings}
			if err := validateIRCommand(command); err != nil {
				t.Fatalf("Command boundary %q must be valid: %v", test.name, err)
			}
			if err := validateIRState(state); err != nil {
				t.Fatalf("State boundary %q must be valid: %v", test.name, err)
			}
		})
	}

	durationMaximum := []uint{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000}
	for _, carrierKHz := range []int{38, 56} {
		command := models.IRCommand{ID: "raw_command", Name: "Raw command", Format: models.OperationFormatRaw, CarrierKHz: carrierKHz, Count: len(durationMaximum), TimingsUS: durationMaximum}
		state := models.IRState{ID: "raw_state", Name: "Raw state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: carrierKHz, Count: len(durationMaximum), TimingsUS: durationMaximum}
		if err := validateIRCommand(command); err != nil {
			t.Fatalf("Command duration maximum with carrier %d must be valid: %v", carrierKHz, err)
		}
		if err := validateIRState(state); err != nil {
			t.Fatalf("State duration maximum with carrier %d must be valid: %v", carrierKHz, err)
		}
	}

	tests := []struct {
		name    string
		timings []uint
		count   int
		message string
	}{
		{name: "count mismatch", timings: []uint{1, 2}, count: 1, message: "invalid raw timings"},
		{name: "zero timing", timings: []uint{1, 0}, count: 2, message: "invalid raw timings"},
		{name: "too many timings", timings: make([]uint, backendir.MaxRawTimings+1), count: backendir.MaxRawTimings + 1, message: "raw timings are too long"},
		{name: "timing over maximum", timings: []uint{backendir.MaxRawTimingUS + 1}, count: 1, message: "raw timing exceeds maximum"},
		{name: "duration over maximum", timings: append(append([]uint(nil), durationMaximum...), 1), count: len(durationMaximum) + 1, message: "raw duration exceeds maximum"},
	}
	for i := range tests[2].timings {
		tests[2].timings[i] = 1
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := models.IRCommand{ID: "raw_command", Name: "Raw command", Format: models.OperationFormatRaw, Count: test.count, TimingsUS: test.timings}
			state := models.IRState{ID: "raw_state", Name: "Raw state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, Count: test.count, TimingsUS: test.timings}
			assertServiceErrorMessage(t, validateIRCommand(command), test.message)
			assertServiceErrorMessage(t, validateIRState(state), test.message)
		})
	}

	negativeCarrier := -1
	assertServiceErrorMessage(t, validateIRCommand(models.IRCommand{ID: "raw_command", Name: "Raw command", Format: models.OperationFormatRaw, CarrierKHz: negativeCarrier, Count: 1, TimingsUS: []uint{1}}), "carrier_khz must be greater than 0")
	assertServiceErrorMessage(t, validateIRState(models.IRState{ID: "raw_state", Name: "Raw state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: negativeCarrier, Count: 1, TimingsUS: []uint{1}}), "carrier_khz must be greater than 0")
}

func TestUnlearnedRawSamplesStateFormatChangesRequireTargetData(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	createUnlearnedRawSamples := func(id string) models.IRState {
		t.Helper()
		state, err := service.CreateState("room", "device", models.IRStateWriteRequest{
			ID:     stringPointer(id),
			Name:   stringPointer(id),
			State:  statePointer(map[string]interface{}{"power": "on"}),
			Format: operationFormatPointer(models.OperationFormatRawSamples),
		})
		if err != nil {
			t.Fatalf("create unlearned raw_samples %q: %v", id, err)
		}
		return state
	}

	codeState := createUnlearnedRawSamples("code_state")
	codeFormat := models.OperationFormatCode
	_, err := service.UpdateState("room", "device", codeState.ID, models.IRStateWriteRequest{Format: &codeFormat, Protocol: stringPointer("PANASONIC_40")})
	assertServiceErrorMessage(t, err, "invalid ir code")
	code, err := service.UpdateState("room", "device", codeState.ID, models.IRStateWriteRequest{Format: &codeFormat, Protocol: stringPointer("PANASONIC_40"), Code: stringPointer("344A90BC2C")})
	if err != nil || code.Format != models.OperationFormatCode || code.Protocol != "PANASONIC_40" || code.Code != "344A90BC2C" {
		t.Fatalf("convert raw_samples to code = %#v, %v", code, err)
	}

	presetState := createUnlearnedRawSamples("preset_state")
	presetFormat := models.OperationFormatPresetCommand
	_, err = service.UpdateState("room", "device", presetState.ID, models.IRStateWriteRequest{Format: &presetFormat})
	assertServiceErrorMessage(t, err, "preset command is not allowed")
	preset, err := service.UpdateState("room", "device", presetState.ID, models.IRStateWriteRequest{Format: &presetFormat, PicoCommand: stringPointer("LIGHT_OFF_CH3")})
	if err != nil || preset.Format != models.OperationFormatPresetCommand || preset.PicoCommand != "LIGHT_OFF_CH3" {
		t.Fatalf("convert raw_samples to preset_command = %#v, %v", preset, err)
	}

	rawState := createUnlearnedRawSamples("raw_state")
	rawFormat := models.OperationFormatRaw
	_, err = service.UpdateState("room", "device", rawState.ID, models.IRStateWriteRequest{Format: &rawFormat})
	assertServiceErrorMessage(t, err, "invalid raw timings")
	count := 3
	timings := []uint{100, 200, 300}
	raw, err := service.UpdateState("room", "device", rawState.ID, models.IRStateWriteRequest{Format: &rawFormat, Count: &count, TimingsUS: &timings})
	if err != nil || raw.Format != models.OperationFormatRaw || raw.Count != count || !reflect.DeepEqual(raw.TimingsUS, timings) {
		t.Fatalf("convert raw_samples to raw = %#v, %v", raw, err)
	}
}

func TestStateWriteValidatesStoredRepeat(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	for _, repeat := range []int{-1, 6} {
		_, err := service.CreateState("room", "device", models.IRStateWriteRequest{
			ID:     stringPointer("invalid_repeat"),
			Name:   stringPointer("Invalid repeat"),
			State:  statePointer(map[string]interface{}{"power": "on"}),
			Format: operationFormatPointer(models.OperationFormatRawSamples),
			Repeat: &repeat,
		})
		assertServiceErrorMessage(t, err, "repeat must be between 1 and 5")
	}
}

func TestResolveCommandCaptureRequest(t *testing.T) {
	samples, maxAttempts, err := resolveCommandCaptureRequest(models.IRCommandCaptureRequest{})
	if err != nil || samples != 3 || maxAttempts != 9 {
		t.Fatalf("defaults = (%d, %d, %v), want (3, 9, nil)", samples, maxAttempts, err)
	}

	for _, sampleCount := range []int{1, 7} {
		samples, maxAttempts, err = resolveCommandCaptureRequest(models.IRCommandCaptureRequest{Samples: &sampleCount, MaxAttempts: &sampleCount})
		if err != nil || samples != sampleCount || maxAttempts != sampleCount {
			t.Fatalf("boundary capture request = (%d, %d, %v)", samples, maxAttempts, err)
		}
	}

	zero := 0
	_, _, err = resolveCommandCaptureRequest(models.IRCommandCaptureRequest{Samples: &zero})
	assertServiceErrorMessage(t, err, "samples must be between 1 and 7")

	eight := 8
	_, _, err = resolveCommandCaptureRequest(models.IRCommandCaptureRequest{Samples: &eight})
	assertServiceErrorMessage(t, err, "samples must be between 1 and 7")

	samples = 3
	two := 2
	_, _, err = resolveCommandCaptureRequest(models.IRCommandCaptureRequest{Samples: &samples, MaxAttempts: &two})
	assertServiceErrorMessage(t, err, "max_attempts must be between samples and 20")

	twentyOne := 21
	_, _, err = resolveCommandCaptureRequest(models.IRCommandCaptureRequest{MaxAttempts: &twentyOne})
	assertServiceErrorMessage(t, err, "max_attempts must be between samples and 20")
}

func TestCommandActionRequestsDistinguishOmittedAndZero(t *testing.T) {
	var captureRequest models.IRCommandCaptureRequest
	if err := json.Unmarshal([]byte(`{"samples":0,"max_attempts":0}`), &captureRequest); err != nil {
		t.Fatalf("unmarshal capture request: %v", err)
	}
	if captureRequest.Samples == nil || *captureRequest.Samples != 0 || captureRequest.MaxAttempts == nil || *captureRequest.MaxAttempts != 0 {
		t.Fatal("capture request must retain explicit zero values")
	}

	var sendRequest models.IRCommandSendRequest
	if err := json.Unmarshal([]byte(`{"repeat":0}`), &sendRequest); err != nil {
		t.Fatalf("unmarshal send request: %v", err)
	}
	if sendRequest.Repeat == nil || *sendRequest.Repeat != 0 {
		t.Fatal("send request must retain an explicit zero value")
	}
}

func TestResolveCommandRepeat(t *testing.T) {
	if repeat, err := resolveCommandRepeat(nil, 0, 0); err != nil || repeat != 1 {
		t.Fatalf("default repeat = (%d, %v), want (1, nil)", repeat, err)
	}
	if repeat, err := resolveCommandRepeat(nil, 3, 4); err != nil || repeat != 3 {
		t.Fatalf("command repeat = (%d, %v), want (3, nil)", repeat, err)
	}
	if repeat, err := resolveCommandRepeat(nil, 0, 4); err != nil || repeat != 4 {
		t.Fatalf("config repeat = (%d, %v), want (4, nil)", repeat, err)
	}

	for _, invalid := range []int{0, -1, 6} {
		_, err := resolveCommandRepeat(&invalid, 3, 4)
		assertServiceErrorMessage(t, err, "repeat must be between 1 and 5")
	}

	assertServiceErrorMessage(t, validateStoredRepeat(-1), "repeat must be between 1 and 5")
	assertServiceErrorMessage(t, validateStoredRepeat(6), "repeat must be between 1 and 5")
	assertServiceErrorMessage(t, validateIRConfig(models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38, DefaultRepeat: 6}), "repeat must be between 1 and 5")
}

func TestCommandFormatContracts(t *testing.T) {
	rawSamples := models.IRCommand{Format: models.OperationFormatRawSamples}
	if err := validateCommandCaptureFormat(rawSamples); err != nil {
		t.Fatalf("raw_samples capture must be supported: %v", err)
	}
	if err := validateCommandSendFormat(rawSamples); err != nil {
		t.Fatalf("raw_samples send must be supported: %v", err)
	}

	for _, format := range []models.OperationFormat{models.OperationFormatRaw, models.OperationFormatCode, models.OperationFormatPresetCommand} {
		assertServiceErrorMessage(t, validateCommandCaptureFormat(models.IRCommand{Format: format}), "ir command format is not supported for capture")
	}
	if err := validateCommandSendFormat(models.IRCommand{Format: models.OperationFormatRaw}); err != nil {
		t.Fatalf("raw send must be supported: %v", err)
	}
	if err := validateCommandSendFormat(models.IRCommand{Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"}); err != nil {
		t.Fatalf("code send planning must be supported: %v", err)
	}
}

func TestCodeNormalizationAndValidationIsSharedByCommandAndState(t *testing.T) {
	command := normalizeIRCommand(models.IRCommand{ID: "code_command", Name: "Code command", Format: models.OperationFormatCode, Protocol: " PANASONIC_40 ", Code: " 344a90bc2c "})
	state := normalizeIRState(models.IRState{ID: "code_state", Name: "Code state", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, Protocol: " PANASONIC_40 ", Code: " 344a90bc2c "})
	if command.Protocol != irCodeProtocolPanasonic40 || command.Code != "344A90BC2C" {
		t.Fatalf("normalized Command code = (%q, %q)", command.Protocol, command.Code)
	}
	if state.Protocol != irCodeProtocolPanasonic40 || state.Code != "344A90BC2C" {
		t.Fatalf("normalized State code = (%q, %q)", state.Protocol, state.Code)
	}
	if err := validateIRCommand(command); err != nil {
		t.Fatalf("normalized Command code must be valid: %v", err)
	}
	if err := validateIRState(state); err != nil {
		t.Fatalf("normalized State code must be valid: %v", err)
	}

	for _, code := range []string{"0000000000", "FFFFFFFFFF"} {
		command.Code = code
		state.Code = code
		if err := validateIRCommand(command); err != nil {
			t.Fatalf("Command boundary code %q must be valid: %v", code, err)
		}
		if err := validateIRState(state); err != nil {
			t.Fatalf("State boundary code %q must be valid: %v", code, err)
		}
	}

	for _, protocol := range []string{"", "NEC", "panasonic_40"} {
		command.Protocol = protocol
		command.Code = "344A90BC2C"
		state.Protocol = protocol
		state.Code = "344A90BC2C"
		assertServiceErrorMessage(t, validateIRCommand(command), "ir code protocol is not supported")
		assertServiceErrorMessage(t, validateIRState(state), "ir code protocol is not supported")
	}

	command.Protocol = irCodeProtocolPanasonic40
	state.Protocol = irCodeProtocolPanasonic40
	for _, code := range []string{"", "344A90BC2", "344A90BC2CC", "344A90BC2G", "0x344A90BC2C"} {
		command.Code = code
		state.Code = code
		assertServiceErrorMessage(t, validateIRCommand(command), "invalid ir code")
		assertServiceErrorMessage(t, validateIRState(state), "invalid ir code")
	}
}

func TestCodeWritesPersistNormalizedCommandAndState(t *testing.T) {
	commandService := newCaptureTestService(t, models.IRCommand{ID: "existing", Name: "Existing", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
	command, err := commandService.CreateCommand("room", "device", models.IRCommandWriteRequest{
		ID:       stringPointer("code_command"),
		Name:     stringPointer("Code command"),
		Format:   operationFormatPointer(models.OperationFormatCode),
		Protocol: stringPointer(" PANASONIC_40 "),
		Code:     stringPointer(" 344a90bc2c "),
	})
	if err != nil || command.Protocol != irCodeProtocolPanasonic40 || command.Code != "344A90BC2C" {
		t.Fatalf("created code Command = %#v, %v", command, err)
	}
	updatedCommandCode := " ffffffffff "
	command, err = commandService.UpdateCommand("room", "device", command.ID, models.IRCommandWriteRequest{Code: &updatedCommandCode})
	if err != nil || command.Code != "FFFFFFFFFF" {
		t.Fatalf("updated code Command = %#v, %v", command, err)
	}

	stateService := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := stateService.SaveStateSchema("room", "device", models.IRStateSchema{"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}}}); err != nil {
		t.Fatalf("save State schema: %v", err)
	}
	state, err := stateService.CreateState("room", "device", models.IRStateWriteRequest{
		ID:       stringPointer("code_state"),
		Name:     stringPointer("Code state"),
		State:    statePointer(map[string]interface{}{"power": "on"}),
		Format:   operationFormatPointer(models.OperationFormatCode),
		Protocol: stringPointer(" PANASONIC_40 "),
		Code:     stringPointer(" 344a90bc2c "),
	})
	if err != nil || state.Protocol != irCodeProtocolPanasonic40 || state.Code != "344A90BC2C" {
		t.Fatalf("created code State = %#v, %v", state, err)
	}
	updatedStateCode := " 0000000000 "
	_, err = stateService.UpdateState("room", "device", state.ID, models.IRStateWriteRequest{Code: &updatedStateCode})
	assertServiceErrorKind(t, err, ErrorConflict)
	sameCode := " 344a90bc2c "
	state, err = stateService.UpdateState("room", "device", state.ID, models.IRStateWriteRequest{Code: &sameCode})
	if err != nil || state.Code != "344A90BC2C" {
		t.Fatalf("identical normalized code PUT failed: %v", err)
	}
}

func TestDuplicateCommandAndStateCreatesReturnConflict(t *testing.T) {
	command := models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"}
	commandService := newCaptureTestService(t, command)
	_, err := commandService.CreateCommand("room", "device", models.IRCommandWriteRequest{
		ID: stringPointer("power"), Name: stringPointer("Power"), Format: operationFormatPointer(models.OperationFormatPresetCommand), PicoCommand: stringPointer("LIGHT_ON_CH3"),
	})
	serviceErr, ok := AsServiceError(err)
	if !ok || serviceErr.Kind != ErrorConflict || serviceErr.Message != "ir command already exists" {
		t.Fatalf("duplicate Command error = %#v, want conflict", err)
	}

	stateService := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := stateService.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save State schema: %v", err)
	}
	stateRequest := models.IRStateWriteRequest{
		ID: stringPointer("power"), Name: stringPointer("Power"), State: statePointer(map[string]interface{}{"power": "on"}), Format: operationFormatPointer(models.OperationFormatPresetCommand), PicoCommand: stringPointer("LIGHT_ON_CH3"),
	}
	if _, err := stateService.CreateState("room", "device", stateRequest); err != nil {
		t.Fatalf("create fixture State: %v", err)
	}
	_, err = stateService.CreateState("room", "device", stateRequest)
	serviceErr, ok = AsServiceError(err)
	if !ok || serviceErr.Kind != ErrorConflict || serviceErr.Message != "ir state already exists" {
		t.Fatalf("duplicate State error = %#v, want conflict", err)
	}
}

func TestPresetCommandAllowlist(t *testing.T) {
	for _, presetCommand := range []string{
		"LIGHT_ON_CH3",
		"LIGHT_FULL_CH3",
		"LIGHT_OFF_CH3",
		"LIGHT_NIGHT_CH3",
		"LIGHT_BRIGHT_UP_CH3",
		"LIGHT_BRIGHT_DOWN_CH3",
	} {
		command := models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand, PicoCommand: presetCommand}
		if err := validateIRCommand(command); err != nil {
			t.Fatalf("preset command %q must be allowed: %v", presetCommand, err)
		}
		if err := validateCommandSendFormat(command); err != nil {
			t.Fatalf("preset command %q must be sendable: %v", presetCommand, err)
		}
		state := models.IRState{ID: "power", Name: "Power", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatPresetCommand, PicoCommand: presetCommand}
		if err := validateIRState(state); err != nil {
			t.Fatalf("State preset command %q must be allowed: %v", presetCommand, err)
		}
	}

	for _, presetCommand := range []string{"", "LIGHT_STATUS_CH3", "UNSAFE_COMMAND"} {
		command := models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand, PicoCommand: presetCommand}
		assertServiceErrorMessage(t, validateIRCommand(command), "preset command is not allowed")
		assertServiceErrorMessage(t, validateCommandSendFormat(command), "preset command is not allowed")
		state := models.IRState{ID: "power", Name: "Power", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatPresetCommand, PicoCommand: presetCommand}
		assertServiceErrorMessage(t, validateIRState(state), "preset command is not allowed")
	}
}

func TestStatePresetCommandCreateAndUpdateUseSharedAllowlist(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if _, err := service.SaveStateSchema("room", "device", models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	state, err := service.CreateState("room", "device", models.IRStateWriteRequest{
		ID:          stringPointer("preset"),
		Name:        stringPointer("Preset"),
		State:       statePointer(map[string]interface{}{"power": "on"}),
		Format:      operationFormatPointer(models.OperationFormatPresetCommand),
		PicoCommand: stringPointer("LIGHT_ON_CH3"),
	})
	if err != nil || state.PicoCommand != "LIGHT_ON_CH3" {
		t.Fatalf("create preset State = %#v, %v", state, err)
	}

	_, err = service.UpdateState("room", "device", state.ID, models.IRStateWriteRequest{PicoCommand: stringPointer("LIGHT_OFF_CH3")})
	assertServiceErrorKind(t, err, ErrorConflict)

	_, err = service.CreateState("room", "device", models.IRStateWriteRequest{
		ID:          stringPointer("invalid_preset"),
		Name:        stringPointer("Invalid preset"),
		State:       statePointer(map[string]interface{}{"power": "off"}),
		Format:      operationFormatPointer(models.OperationFormatPresetCommand),
		PicoCommand: stringPointer("LIGHT_STATUS_CH3"),
	})
	assertServiceErrorMessage(t, err, "preset command is not allowed")

	_, err = service.UpdateState("room", "device", state.ID, models.IRStateWriteRequest{PicoCommand: stringPointer("UNSAFE_COMMAND")})
	assertServiceErrorKind(t, err, ErrorConflict)
	saved, loadErr := service.irAdapterRepository.LoadState("room", "device", state.ID)
	if loadErr != nil || saved.PicoCommand != "LIGHT_ON_CH3" {
		t.Fatalf("invalid update changed State = %#v, %v", saved, loadErr)
	}
}

func TestCaptureCommandSavesLearnedDataOnlyAfterSuccess(t *testing.T) {
	original := models.IRCommand{
		ID:        "power",
		Name:      "Power",
		Format:    models.OperationFormatRawSamples,
		Count:     2,
		TimingsUS: []uint{1, 2},
		Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 2, TimingsUS: []uint{1, 2}}},
	}
	service := newCaptureTestService(t, original)
	results := []models.IRCapture{
		{Attempt: 1, Valid: false, Count: 0, Error: "no signal"},
		{Attempt: 2, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}},
		{Attempt: 3, Valid: true, Count: 4, TimingsUS: []uint{110, 210, 310, 410}},
		{Attempt: 4, Valid: true, Count: 4, TimingsUS: []uint{120, 220, 320, 420}},
	}
	service.capture = func(attempt int) models.IRCapture {
		return results[attempt-1]
	}
	samples, maxAttempts := 3, 4

	response, err := service.CaptureCommand("room", "device", "power", models.IRCommandCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
	if err != nil {
		t.Fatalf("capture command: %v", err)
	}
	command := response.Command
	if !response.OK || response.RequiredSamples != 3 || response.ValidCaptures != 3 {
		t.Fatalf("capture response = %#v", response)
	}
	if command.Count != 4 || !reflect.DeepEqual(command.TimingsUS, []uint{110, 210, 310, 410}) || len(command.Captures) != 4 {
		t.Fatalf("learned command = %#v", command)
	}

	saved, err := service.irAdapterRepository.LoadCommand("room", "device", "power")
	if err != nil {
		t.Fatalf("load saved command: %v", err)
	}
	if !reflect.DeepEqual(saved, command) {
		t.Fatalf("saved command = %#v, want %#v", saved, command)
	}
}

func TestCaptureCommandPreservesExistingDataOnFailure(t *testing.T) {
	original := models.IRCommand{
		ID:        "power",
		Name:      "Power",
		Format:    models.OperationFormatRawSamples,
		Count:     2,
		TimingsUS: []uint{1, 2},
		Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 2, TimingsUS: []uint{1, 2}}},
	}
	service := newCaptureTestService(t, original)
	service.capture = func(attempt int) models.IRCapture {
		return models.IRCapture{Attempt: attempt, Valid: false, Count: 0, Error: "no signal"}
	}
	samples, maxAttempts := 1, 2

	_, err := service.CaptureCommand("room", "device", "power", models.IRCommandCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
	assertServiceErrorMessage(t, err, "not enough valid captures")
	assertServiceErrorKind(t, err, ErrorCaptureInsufficient)

	saved, loadErr := service.irAdapterRepository.LoadCommand("room", "device", "power")
	if loadErr != nil {
		t.Fatalf("load command after failed capture: %v", loadErr)
	}
	if !reflect.DeepEqual(saved, original) {
		t.Fatalf("failed capture changed saved command: %#v", saved)
	}
}

func TestCaptureCommandRejectsUnsupportedFormatWithoutCapturing(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
	captureCalls := 0
	service.capture = func(attempt int) models.IRCapture {
		captureCalls++
		return models.IRCapture{}
	}

	_, err := service.CaptureCommand("room", "device", "power", models.IRCommandCaptureRequest{})
	assertServiceErrorMessage(t, err, "ir command format is not supported for capture")
	if captureCalls != 0 {
		t.Fatalf("capture called %d times for an unsupported format", captureCalls)
	}
}

func TestCaptureStateSavesLearnedDataOnlyAfterSuccess(t *testing.T) {
	original := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	service := newStateCaptureTestService(t, original)
	results := []models.IRCapture{
		{Attempt: 1, Valid: false, Count: 0, Error: "no signal"},
		{Attempt: 2, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}},
		{Attempt: 3, Valid: true, Count: 4, TimingsUS: []uint{110, 210, 310, 410}},
		{Attempt: 4, Valid: true, Count: 4, TimingsUS: []uint{120, 220, 320, 420}},
	}
	service.capture = func(attempt int) models.IRCapture {
		return results[attempt-1]
	}
	samples, maxAttempts := 3, 4

	response, err := service.CaptureState("room", "device", original.ID, models.IRStateCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
	if err != nil {
		t.Fatalf("capture state: %v", err)
	}
	state := response.State
	if !response.OK || response.RequiredSamples != 3 || response.ValidCaptures != 3 {
		t.Fatalf("capture response = %#v", response)
	}
	if state.Count != 4 || !reflect.DeepEqual(state.TimingsUS, []uint{110, 210, 310, 410}) || len(state.Captures) != 4 {
		t.Fatalf("learned state = %#v", state)
	}

	saved, err := service.irAdapterRepository.LoadState("room", "device", original.ID)
	if err != nil {
		t.Fatalf("load saved state: %v", err)
	}
	if !reflect.DeepEqual(saved, state) {
		t.Fatalf("saved state = %#v, want %#v", saved, state)
	}
}

func TestCaptureStatePreservesExistingDataOnFailure(t *testing.T) {
	original := models.IRState{
		ID:        "cool_26_auto",
		Name:      "Cool 26 Auto",
		State:     map[string]interface{}{"power": "on"},
		Format:    models.OperationFormatRawSamples,
		Count:     2,
		TimingsUS: []uint{1, 2},
		Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 2, TimingsUS: []uint{1, 2}}},
	}
	service := newStateCaptureTestService(t, original)
	service.capture = func(attempt int) models.IRCapture {
		return models.IRCapture{Attempt: attempt, Valid: false, Count: 0, Error: "no signal"}
	}
	samples, maxAttempts := 1, 2

	_, err := service.CaptureState("room", "device", original.ID, models.IRStateCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
	assertServiceErrorMessage(t, err, "not enough valid captures")
	assertServiceErrorKind(t, err, ErrorCaptureInsufficient)

	saved, loadErr := service.irAdapterRepository.LoadState("room", "device", original.ID)
	if loadErr != nil {
		t.Fatalf("load state after failed capture: %v", loadErr)
	}
	if !reflect.DeepEqual(saved, original) {
		t.Fatalf("failed capture changed saved state: %#v", saved)
	}
}

func TestUnsafeCapturePreservesExistingCommandAndStateLearningData(t *testing.T) {
	t.Run("Command", func(t *testing.T) {
		original := models.IRCommand{
			ID:        "power",
			Name:      "Power",
			Format:    models.OperationFormatRawSamples,
			Count:     2,
			TimingsUS: []uint{100, 200},
			Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 2, TimingsUS: []uint{100, 200}}},
		}
		service := newCaptureTestService(t, original)
		service.capture = func(attempt int) models.IRCapture {
			return models.IRCapture{Attempt: attempt, OK: true, Valid: true, Count: 1, TimingsUS: []uint{backendir.MaxRawTimingUS + 1}}
		}
		samples, maxAttempts := 1, 1

		_, err := service.CaptureCommand("room", "device", original.ID, models.IRCommandCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
		assertServiceErrorMessage(t, err, "not enough valid captures")
		saved, loadErr := service.irAdapterRepository.LoadCommand("room", "device", original.ID)
		if loadErr != nil {
			t.Fatalf("load command after unsafe capture: %v", loadErr)
		}
		if !reflect.DeepEqual(saved, original) {
			t.Fatalf("unsafe capture changed saved Command: %#v", saved)
		}
	})

	t.Run("State", func(t *testing.T) {
		original := models.IRState{
			ID:        "cool_26_auto",
			Name:      "Cool 26 Auto",
			State:     map[string]interface{}{"power": "on"},
			Format:    models.OperationFormatRawSamples,
			Count:     2,
			TimingsUS: []uint{100, 200},
			Captures:  []models.IRCapture{{Attempt: 1, Valid: true, Count: 2, TimingsUS: []uint{100, 200}}},
		}
		service := newStateCaptureTestService(t, original)
		service.capture = func(attempt int) models.IRCapture {
			return models.IRCapture{Attempt: attempt, OK: true, Valid: true, Count: 1, TimingsUS: []uint{backendir.MaxRawTimingUS + 1}}
		}
		samples, maxAttempts := 1, 1

		_, err := service.CaptureState("room", "device", original.ID, models.IRStateCaptureRequest{Samples: &samples, MaxAttempts: &maxAttempts})
		assertServiceErrorMessage(t, err, "not enough valid captures")
		saved, loadErr := service.irAdapterRepository.LoadState("room", "device", original.ID)
		if loadErr != nil {
			t.Fatalf("load State after unsafe capture: %v", loadErr)
		}
		if !reflect.DeepEqual(saved, original) {
			t.Fatalf("unsafe capture changed saved State: %#v", saved)
		}
	})
}

func TestCaptureStateRejectsMissingSchemaAndUnsupportedFormatsWithoutCapturing(t *testing.T) {
	missingSchemaState := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	missingSchemaService := newStateTestService(t, models.IRRemoteTypeStateful, true)
	if err := missingSchemaService.irAdapterRepository.SaveState("room", "device", missingSchemaState); err != nil {
		t.Fatalf("save state without schema: %v", err)
	}
	_, err := missingSchemaService.CaptureState("room", "device", missingSchemaState.ID, models.IRStateCaptureRequest{})
	assertServiceErrorMessage(t, err, "ir state schema not found")

	for _, format := range []models.OperationFormat{
		models.OperationFormatRaw,
		models.OperationFormatCode,
		models.OperationFormatPresetCommand,
	} {
		state := models.IRState{
			ID:     "state_" + string(format),
			Name:   "State " + string(format),
			State:  map[string]interface{}{"power": "on"},
			Format: format,
		}
		service := newStateCaptureTestService(t, state)
		captureCalls := 0
		service.capture = func(attempt int) models.IRCapture {
			captureCalls++
			return models.IRCapture{}
		}

		_, err := service.CaptureState("room", "device", state.ID, models.IRStateCaptureRequest{})
		assertServiceErrorMessage(t, err, "ir state format is not supported for capture")
		if captureCalls != 0 {
			t.Fatalf("capture called %d times for unsupported format %q", captureCalls, format)
		}
	}
}

func TestResolveStateCaptureRequest(t *testing.T) {
	samples, maxAttempts, err := resolveStateCaptureRequest(models.IRStateCaptureRequest{})
	if err != nil || samples != 3 || maxAttempts != 9 {
		t.Fatalf("defaults = (%d, %d, %v), want (3, 9, nil)", samples, maxAttempts, err)
	}

	zero := 0
	_, _, err = resolveStateCaptureRequest(models.IRStateCaptureRequest{Samples: &zero})
	assertServiceErrorMessage(t, err, "samples must be between 1 and 7")
}

func TestSendStateRawSamplesUsesRepresentativeTimings(t *testing.T) {
	state := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
		Repeat: 2,
		Captures: []models.IRCapture{
			{Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}},
			{Valid: true, Count: 4, TimingsUS: []uint{110, 210, 310, 410}},
			{Valid: true, Count: 4, TimingsUS: []uint{120, 220, 320, 420}},
		},
	}
	service := newStateCaptureTestService(t, state)
	var sentTimings [][]uint
	service.sendRaw = func(timings []uint) (string, error) {
		sentTimings = append(sentTimings, append([]uint(nil), timings...))
		return "ok", nil
	}

	response, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
	if err != nil {
		t.Fatalf("send state: %v", err)
	}
	if !response.OK || response.RoomID != "room" || response.DeviceID != "device" || response.StateID != state.ID || response.Format != models.OperationFormatRawSamples || response.Repeat != 2 || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 {
		t.Fatalf("response = %#v", response)
	}
	if len(sentTimings) != 2 || !reflect.DeepEqual(sentTimings[0], []uint{110, 210, 310, 410}) || !reflect.DeepEqual(sentTimings[1], sentTimings[0]) {
		t.Fatalf("sent timings = %#v", sentTimings)
	}
}

func TestSendStatePresetCommandUsesSharedExecutorAndRepeat(t *testing.T) {
	state := models.IRState{
		ID:          "preset",
		Name:        "Preset",
		State:       map[string]interface{}{"power": "on"},
		Format:      models.OperationFormatPresetCommand,
		PicoCommand: "LIGHT_ON_CH3",
		Repeat:      2,
	}
	service := newStateCaptureTestService(t, state)
	var sentCommands []string
	service.sendPresetCommand = func(command string) (string, error) {
		sentCommands = append(sentCommands, command)
		return `{"ok":true}`, nil
	}

	requestRepeat := 3
	response, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{Repeat: &requestRepeat})
	if err != nil {
		t.Fatalf("send preset State: %v", err)
	}
	wantCommands := []string{"LIGHT_ON_CH3", "LIGHT_ON_CH3", "LIGHT_ON_CH3"}
	if !response.OK || response.Format != models.OperationFormatPresetCommand || response.Repeat != 3 || response.RequestedRepeats != 3 || response.CompletedRepeats != 3 || response.DeliveryState != models.IRDeliveryStateSent || !reflect.DeepEqual(sentCommands, wantCommands) {
		t.Fatalf("response = %#v, sent commands = %#v", response, sentCommands)
	}

	sentCommands = nil
	response, err = service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
	if err != nil || response.Repeat != 2 || len(sentCommands) != 2 {
		t.Fatalf("State repeat fallback = %#v, commands = %#v, error = %v", response, sentCommands, err)
	}

	state.Repeat = 0
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatalf("clear State repeat: %v", err)
	}
	if err := service.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38, DefaultRepeat: 4}); err != nil {
		t.Fatalf("save config repeat: %v", err)
	}
	sentCommands = nil
	response, err = service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
	if err != nil || response.Repeat != 4 || len(sentCommands) != 4 {
		t.Fatalf("config repeat fallback = %#v, commands = %#v, error = %v", response, sentCommands, err)
	}

	if err := service.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatalf("clear config repeat: %v", err)
	}
	sentCommands = nil
	response, err = service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
	if err != nil || response.Repeat != 1 || len(sentCommands) != 1 {
		t.Fatalf("default repeat fallback = %#v, commands = %#v, error = %v", response, sentCommands, err)
	}
}

func TestSendStateRejectsStoredUnallowedPresetBeforeSending(t *testing.T) {
	for _, preset := range []string{"", "LIGHT_STATUS_CH3", "UNSAFE_COMMAND"} {
		t.Run(preset, func(t *testing.T) {
			state := models.IRState{
				ID:          "preset",
				Name:        "Preset",
				State:       map[string]interface{}{"power": "on"},
				Format:      models.OperationFormatPresetCommand,
				PicoCommand: preset,
			}
			service := newStateCaptureTestService(t, state)
			calls := 0
			service.sendPresetCommand = func(string) (string, error) {
				calls++
				return "", nil
			}

			_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
			assertServiceErrorMessage(t, err, "preset command is not allowed")
			var serviceErr *ServiceError
			if !errors.As(err, &serviceErr) {
				t.Fatalf("error = %v, want *ServiceError", err)
			}
			payload, ok := serviceErr.Payload.(models.IRSendErrorResponse)
			if !ok || payload.DeliveryState != models.IRDeliveryStateNotSent || payload.CompletedRepeats != 0 || payload.RetryableWith == nil {
				t.Fatalf("preset rejection payload = %#v", serviceErr.Payload)
			}
			if calls != 0 {
				t.Fatalf("preset sender called %d times, want 0", calls)
			}
		})
	}
}

func TestSendStateResolvesRepeatFromRequestStateConfigAndDefault(t *testing.T) {
	state := models.IRState{
		ID:        "cool_26_auto",
		Name:      "Cool 26 Auto",
		State:     map[string]interface{}{"power": "on"},
		Format:    models.OperationFormatRawSamples,
		TimingsUS: []uint{100, 200, 300, 400},
	}
	service := newStateCaptureTestService(t, state)
	if err := service.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38, DefaultRepeat: 4}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	callCount := 0
	service.sendRaw = func([]uint) (string, error) {
		callCount++
		return "ok", nil
	}

	assertStateSendRepeat(t, service, state.ID, models.IRStateSendRequest{}, 4, &callCount)

	state.Repeat = 2
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatalf("save state repeat: %v", err)
	}
	assertStateSendRepeat(t, service, state.ID, models.IRStateSendRequest{}, 2, &callCount)

	requestRepeat := 3
	assertStateSendRepeat(t, service, state.ID, models.IRStateSendRequest{Repeat: &requestRepeat}, 3, &callCount)

	state.Repeat = 0
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatalf("clear state repeat: %v", err)
	}
	if err := service.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatalf("clear config repeat: %v", err)
	}
	assertStateSendRepeat(t, service, state.ID, models.IRStateSendRequest{}, 1, &callCount)
}

func TestSendStateRejectsUnlearnedUnsupportedInvalidRepeatAndControllerError(t *testing.T) {
	baseState := models.IRState{
		ID:     "cool_26_auto",
		Name:   "Cool 26 Auto",
		State:  map[string]interface{}{"power": "on"},
		Format: models.OperationFormatRawSamples,
	}
	service := newStateCaptureTestService(t, baseState)
	_, err := service.SendState("room", "device", baseState.ID, models.IRStateSendRequest{})
	assertServiceErrorMessage(t, err, string(models.IRStateActionErrorNotLearned))
	assertServiceErrorKind(t, err, ErrorConflict)

	for _, format := range []models.OperationFormat{models.OperationFormatPresetCommand} {
		state := baseState
		state.ID = "state_" + string(format)
		state.Format = format
		service := newStateCaptureTestService(t, state)
		_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
		assertServiceErrorMessage(t, err, "preset command is not allowed")
		assertServiceErrorKind(t, err, ErrorInvalidRequest)
	}

	learnedState := baseState
	learnedState.TimingsUS = []uint{100, 200, 300, 400}
	service = newStateCaptureTestService(t, learnedState)
	zero := 0
	_, err = service.SendState("room", "device", learnedState.ID, models.IRStateSendRequest{Repeat: &zero})
	assertServiceErrorMessage(t, err, "repeat must be between 1 and 5")

	service.sendRaw = func([]uint) (string, error) { return "", errors.New("socket closed") }
	one := 1
	_, err = service.SendState("room", "device", learnedState.ID, models.IRStateSendRequest{Repeat: &one})
	assertServiceErrorMessage(t, err, string(models.IRStateActionErrorControllerDaemon))
	assertServiceErrorKind(t, err, ErrorController)
}

func TestSendStateRejectsExecutionModeForNonCodeBeforeSending(t *testing.T) {
	state := models.IRState{
		ID:        "cool_26_auto",
		Name:      "Cool 26 Auto",
		State:     map[string]interface{}{"power": "on"},
		Format:    models.OperationFormatRawSamples,
		TimingsUS: []uint{100, 200, 300, 400},
	}
	service := newStateCaptureTestService(t, state)
	sendCalls := 0
	service.sendRaw = func([]uint) (string, error) {
		sendCalls++
		return "ok", nil
	}
	backendRaw := models.IRExecutionModeBackendRaw

	_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{ExecutionMode: &backendRaw})
	assertServiceErrorMessage(t, err, "invalid ir execution mode")
	if sendCalls != 0 {
		t.Fatalf("send called %d times, want 0", sendCalls)
	}
}

func TestSendStateRejectsUnsafeStoredRawSamplesBeforeSending(t *testing.T) {
	state := models.IRState{
		ID:        "cool_26_auto",
		Name:      "Cool 26 Auto",
		State:     map[string]interface{}{"power": "on"},
		Format:    models.OperationFormatRawSamples,
		Count:     1,
		TimingsUS: []uint{backendir.MaxRawTimingUS + 1},
	}
	service := newStateCaptureTestService(t, state)
	sendCalls := 0
	service.sendRaw = func([]uint) (string, error) {
		sendCalls++
		return "ok", nil
	}

	_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
	assertServiceErrorMessage(t, err, "raw timing exceeds maximum")
	if sendCalls != 0 {
		t.Fatalf("send called %d times, want 0", sendCalls)
	}
}

func TestSendCommandRawSamplesUsesRepresentativeTimingsAndRepeat(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{
		ID:     "power",
		Name:   "Power",
		Format: models.OperationFormatRawSamples,
		Repeat: 2,
		Captures: []models.IRCapture{
			{Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}},
			{Valid: true, Count: 4, TimingsUS: []uint{110, 210, 310, 410}},
			{Valid: true, Count: 4, TimingsUS: []uint{120, 220, 320, 420}},
		},
	})
	var sentTimings [][]uint
	service.sendRaw = func(timings []uint) (string, error) {
		sentTimings = append(sentTimings, append([]uint(nil), timings...))
		return "ok", nil
	}

	response, err := service.SendCommand("room", "device", "power", models.IRCommandSendRequest{})
	if err != nil {
		t.Fatalf("send raw_samples command: %v", err)
	}
	if !response.OK || response.Repeat != 2 || response.Format != models.OperationFormatRawSamples || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 {
		t.Fatalf("send response = %#v", response)
	}
	if len(sentTimings) != 2 || !reflect.DeepEqual(sentTimings[0], []uint{110, 210, 310, 410}) || !reflect.DeepEqual(sentTimings[1], sentTimings[0]) {
		t.Fatalf("sent timings = %#v", sentTimings)
	}
}

func TestSendCommandAndStateRawUseExistingRawSender(t *testing.T) {
	timings := []uint{3500, 1750, 450, 10000}

	t.Run("command", func(t *testing.T) {
		command := models.IRCommand{ID: "raw", Name: "Raw", Format: models.OperationFormatRaw, CarrierKHz: 38, Count: len(timings), TimingsUS: timings, Repeat: 2}
		service := newCaptureTestService(t, command)
		var sent [][]uint
		service.sendRaw = func(got []uint) (string, error) {
			sent = append(sent, append([]uint(nil), got...))
			return `{"ok":true}`, nil
		}

		response, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{})
		if err != nil {
			t.Fatalf("send raw Command: %v", err)
		}
		if response.Format != models.OperationFormatRaw || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 || !reflect.DeepEqual(sent, [][]uint{timings, timings}) {
			t.Fatalf("response = %#v, sent = %#v", response, sent)
		}
	})

	t.Run("state", func(t *testing.T) {
		state := models.IRState{ID: "raw", Name: "Raw", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: 38, Count: len(timings), TimingsUS: timings, Repeat: 2}
		service := newStateCaptureTestService(t, state)
		var sent [][]uint
		service.sendRaw = func(got []uint) (string, error) {
			sent = append(sent, append([]uint(nil), got...))
			return `{"ok":true}`, nil
		}

		response, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
		if err != nil {
			t.Fatalf("send raw State: %v", err)
		}
		if response.Format != models.OperationFormatRaw || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 || !reflect.DeepEqual(sent, [][]uint{timings, timings}) {
			t.Fatalf("response = %#v, sent = %#v", response, sent)
		}
	})
}

func TestSendCommandAndStateCodeBackendRawUsesEncoder(t *testing.T) {
	const code = "344A90BC2C"
	wantTimings, err := backendir.EncodeCodeToRaw("PANASONIC_40", code)
	if err != nil {
		t.Fatalf("encode expected timings: %v", err)
	}
	backendRaw := models.IRExecutionModeBackendRaw

	t.Run("command", func(t *testing.T) {
		command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, CarrierKHz: 38, Protocol: "PANASONIC_40", Code: code, Repeat: 2}
		service := newCaptureTestService(t, command)
		var sent [][]uint
		service.sendRaw = func(got []uint) (string, error) {
			sent = append(sent, append([]uint(nil), got...))
			return `{"ok":true}`, nil
		}

		response, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &backendRaw})
		if err != nil {
			t.Fatalf("send backend_raw Command: %v", err)
		}
		if response.Format != models.OperationFormatCode || response.Protocol != "PANASONIC_40" || response.ExecutionMode != backendRaw || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 || !reflect.DeepEqual(sent, [][]uint{wantTimings, wantTimings}) {
			t.Fatalf("response = %#v, sent timings = %#v", response, sent)
		}
	})

	t.Run("state", func(t *testing.T) {
		state := models.IRState{ID: "code", Name: "Code", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, CarrierKHz: 38, Protocol: "PANASONIC_40", Code: code, Repeat: 2}
		service := newStateCaptureTestService(t, state)
		var sent [][]uint
		service.sendRaw = func(got []uint) (string, error) {
			sent = append(sent, append([]uint(nil), got...))
			return `{"ok":true}`, nil
		}

		response, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{ExecutionMode: &backendRaw})
		if err != nil {
			t.Fatalf("send backend_raw State: %v", err)
		}
		if response.Format != models.OperationFormatCode || response.Protocol != "PANASONIC_40" || response.ExecutionMode != backendRaw || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 2 || response.CompletedRepeats != 2 || !reflect.DeepEqual(sent, [][]uint{wantTimings, wantTimings}) {
			t.Fatalf("response = %#v, sent timings = %#v", response, sent)
		}
	})
}

func TestSendCommandAndStateRejectUnsupportedRawCarrierWithoutSending(t *testing.T) {
	backendRaw := models.IRExecutionModeBackendRaw
	commandTests := []struct {
		name    string
		command models.IRCommand
		request models.IRCommandSendRequest
	}{
		{name: "raw_samples", command: models.IRCommand{ID: "raw_samples", Name: "Raw samples", Format: models.OperationFormatRawSamples, CarrierKHz: 56, TimingsUS: []uint{100, 200}}},
		{name: "raw", command: models.IRCommand{ID: "raw", Name: "Raw", Format: models.OperationFormatRaw, CarrierKHz: 56, Count: 2, TimingsUS: []uint{100, 200}}},
		{name: "backend_raw", command: models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, CarrierKHz: 56, Protocol: "PANASONIC_40", Code: "344A90BC2C"}, request: models.IRCommandSendRequest{ExecutionMode: &backendRaw}},
	}
	for _, tt := range commandTests {
		t.Run("command_"+tt.name, func(t *testing.T) {
			service := newCaptureTestService(t, tt.command)
			calls := 0
			service.sendRaw = func([]uint) (string, error) { calls++; return "", nil }
			_, err := service.SendCommand("room", "device", tt.command.ID, tt.request)
			assertCarrierSendRejection(t, err)
			if calls != 0 {
				t.Fatalf("raw sender called %d times, want 0", calls)
			}
			saved, loadErr := service.irAdapterRepository.LoadCommand("room", "device", tt.command.ID)
			if loadErr != nil || saved.CarrierKHz != 56 {
				t.Fatalf("stored Command changed after rejection: %#v, %v", saved, loadErr)
			}
		})
	}

	stateTests := []struct {
		name    string
		state   models.IRState
		request models.IRStateSendRequest
	}{
		{name: "raw_samples", state: models.IRState{ID: "raw_samples", Name: "Raw samples", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRawSamples, CarrierKHz: 56, TimingsUS: []uint{100, 200}}},
		{name: "raw", state: models.IRState{ID: "raw", Name: "Raw", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: 56, Count: 2, TimingsUS: []uint{100, 200}}},
		{name: "backend_raw", state: models.IRState{ID: "code", Name: "Code", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, CarrierKHz: 56, Protocol: "PANASONIC_40", Code: "344A90BC2C"}, request: models.IRStateSendRequest{ExecutionMode: &backendRaw}},
	}
	for _, tt := range stateTests {
		t.Run("state_"+tt.name, func(t *testing.T) {
			service := newStateCaptureTestService(t, tt.state)
			calls := 0
			service.sendRaw = func([]uint) (string, error) { calls++; return "", nil }
			_, err := service.SendState("room", "device", tt.state.ID, tt.request)
			assertCarrierSendRejection(t, err)
			if calls != 0 {
				t.Fatalf("raw sender called %d times, want 0", calls)
			}
			saved, loadErr := service.irAdapterRepository.LoadState("room", "device", tt.state.ID)
			if loadErr != nil || saved.CarrierKHz != 56 {
				t.Fatalf("stored State changed after rejection: %#v, %v", saved, loadErr)
			}
		})
	}
}

func assertCarrierSendRejection(t *testing.T, err error) {
	t.Helper()
	assertServiceErrorMessage(t, err, "ir carrier is not supported for execution")
	assertServiceErrorKind(t, err, ErrorConflict)
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("error = %v, want *ServiceError", err)
	}
	payload, ok := serviceErr.Payload.(models.IRSendErrorResponse)
	if !ok || payload.DeliveryState != models.IRDeliveryStateNotSent || payload.CompletedRepeats != 0 || payload.RequestedRepeats != 1 || payload.RetryableWith == nil {
		t.Fatalf("carrier rejection payload = %#v", serviceErr.Payload)
	}
}

func TestSendCommandAndStateRawRevalidateSafetyBeforeSending(t *testing.T) {
	unsafeTimings := []uint{backendir.MaxRawTimingUS + 1}

	t.Run("command", func(t *testing.T) {
		command := models.IRCommand{ID: "raw", Name: "Raw", Format: models.OperationFormatRaw, CarrierKHz: 38, Count: 1, TimingsUS: unsafeTimings}
		service := newCaptureTestService(t, command)
		calls := 0
		service.sendRaw = func([]uint) (string, error) { calls++; return "", nil }

		_, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{})
		assertServiceErrorMessage(t, err, "raw timing exceeds maximum")
		if calls != 0 {
			t.Fatalf("raw sender called %d times, want 0", calls)
		}
	})

	t.Run("state", func(t *testing.T) {
		state := models.IRState{ID: "raw", Name: "Raw", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatRaw, CarrierKHz: 38, Count: 1, TimingsUS: unsafeTimings}
		service := newStateCaptureTestService(t, state)
		calls := 0
		service.sendRaw = func([]uint) (string, error) { calls++; return "", nil }

		_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{})
		assertServiceErrorMessage(t, err, "raw timing exceeds maximum")
		if calls != 0 {
			t.Fatalf("raw sender called %d times, want 0", calls)
		}
	})
}

func TestBackendRawControllerFailureDoesNotFallback(t *testing.T) {
	backendRaw := models.IRExecutionModeBackendRaw
	failure := controller_client.CommandResult{
		FailureStage:   controller_client.FailureStageRead,
		BytesWritten:   32,
		WriteCompleted: true,
		Err:            errors.New("read timeout"),
	}

	t.Run("command", func(t *testing.T) {
		command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, CarrierKHz: 38, Protocol: "PANASONIC_40", Code: "344A90BC2C"}
		service := newCaptureTestService(t, command)
		rawCalls, fallbackCalls := 0, 0
		service.sendRawResult = func([]uint) (controller_client.CommandResult, error) { rawCalls++; return failure, failure.Err }
		service.sendPresetResult = func(string) (controller_client.CommandResult, error) {
			fallbackCalls++
			return controller_client.CommandResult{}, nil
		}

		_, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &backendRaw})
		assertTypedSendFailure(t, err, models.IRDeliveryStateUnknown, 1, 0)
		if rawCalls != 1 || fallbackCalls != 0 {
			t.Fatalf("sender calls = raw %d, fallback %d; want 1, 0", rawCalls, fallbackCalls)
		}
	})

	t.Run("state", func(t *testing.T) {
		state := models.IRState{ID: "code", Name: "Code", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, CarrierKHz: 38, Protocol: "PANASONIC_40", Code: "344A90BC2C"}
		service := newStateCaptureTestService(t, state)
		rawCalls, fallbackCalls := 0, 0
		service.sendRawResult = func([]uint) (controller_client.CommandResult, error) { rawCalls++; return failure, failure.Err }
		service.sendPresetResult = func(string) (controller_client.CommandResult, error) {
			fallbackCalls++
			return controller_client.CommandResult{}, nil
		}

		_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{ExecutionMode: &backendRaw})
		assertTypedSendFailure(t, err, models.IRDeliveryStateUnknown, 1, 0)
		if rawCalls != 1 || fallbackCalls != 0 {
			t.Fatalf("sender calls = raw %d, fallback %d; want 1, 0", rawCalls, fallbackCalls)
		}
	})
}

func TestSendCommandPresetUsesAllowlistedCommand(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "on", Name: "On", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
	var sentCommands []string
	service.sendPresetCommand = func(command string) (string, error) {
		sentCommands = append(sentCommands, command)
		return "ok", nil
	}
	repeat := 1

	response, err := service.SendCommand("room", "device", "on", models.IRCommandSendRequest{Repeat: &repeat})
	if err != nil {
		t.Fatalf("send preset command: %v", err)
	}
	if !response.OK || response.Repeat != 1 || response.DeliveryState != models.IRDeliveryStateSent || response.RequestedRepeats != 1 || response.CompletedRepeats != 1 || !reflect.DeepEqual(sentCommands, []string{"LIGHT_ON_CH3"}) {
		t.Fatalf("response = %#v, sent commands = %#v", response, sentCommands)
	}
}

func TestCommandAndStateSendStopAfterTypedControllerFailure(t *testing.T) {
	tests := []struct {
		name          string
		failureResult controller_client.CommandResult
		wantDelivery  models.IRDeliveryState
	}{
		{
			name: "success then pre-send reject",
			failureResult: controller_client.CommandResult{
				FailureStage: controller_client.FailureStageRemoteReject,
				RemoteResponse: &controller_client.RemoteResponse{
					OK:        false,
					Error:     "unsupported command",
					ErrorCode: models.IRWireErrorCodeUnsupportedCommand,
				},
				Err: controller_client.ErrRemoteRejected,
			},
			wantDelivery: models.IRDeliveryStatePartiallySent,
		},
		{
			name: "success then write-complete read timeout",
			failureResult: controller_client.CommandResult{
				FailureStage:   controller_client.FailureStageRead,
				BytesWritten:   len("IR_SEND_RAW:100,200,300,400\n"),
				WriteCompleted: true,
				Err:            errors.New("read timeout"),
			},
			wantDelivery: models.IRDeliveryStateUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("command", func(t *testing.T) {
				service := newCaptureTestService(t, models.IRCommand{ID: "on", Name: "On", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
				calls := 0
				sequence := typedResultSequence(&calls, tt.failureResult)
				service.sendPresetResult = func(string) (controller_client.CommandResult, error) { return sequence() }
				repeat := 3

				_, err := service.SendCommand("room", "device", "on", models.IRCommandSendRequest{Repeat: &repeat})
				assertTypedSendFailure(t, err, tt.wantDelivery, 3, 1)
				if calls != 2 {
					t.Fatalf("controller calls = %d, want 2; executor must not retry after failure", calls)
				}
			})

			t.Run("state", func(t *testing.T) {
				state := models.IRState{
					ID:        "cool_26_auto",
					Name:      "Cool 26 Auto",
					State:     map[string]interface{}{"power": "on"},
					Format:    models.OperationFormatRawSamples,
					TimingsUS: []uint{100, 200, 300, 400},
				}
				service := newStateCaptureTestService(t, state)
				calls := 0
				sequence := typedResultSequence(&calls, tt.failureResult)
				service.sendRawResult = func([]uint) (controller_client.CommandResult, error) {
					return sequence()
				}
				repeat := 3

				_, err := service.SendState("room", "device", state.ID, models.IRStateSendRequest{Repeat: &repeat})
				assertTypedSendFailure(t, err, tt.wantDelivery, 3, 1)
				if calls != 2 {
					t.Fatalf("controller calls = %d, want 2; executor must not retry after failure", calls)
				}
			})
		})
	}
}

func typedResultSequence(calls *int, failure controller_client.CommandResult) func() (controller_client.CommandResult, error) {
	return func() (controller_client.CommandResult, error) {
		(*calls)++
		if *calls == 1 {
			return controller_client.CommandResult{
				BytesWritten:   1,
				WriteCompleted: true,
				RemoteResponse: &controller_client.RemoteResponse{OK: true},
			}, nil
		}
		return failure, failure.Err
	}
}

func assertTypedSendFailure(t *testing.T, err error, wantDelivery models.IRDeliveryState, wantRequested int, wantCompleted int) {
	t.Helper()
	assertServiceErrorKind(t, err, ErrorController)
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("error = %v, want *ServiceError", err)
	}
	payload, ok := serviceErr.Payload.(models.IRSendErrorResponse)
	if !ok {
		t.Fatalf("payload = %#v, want IRSendErrorResponse", serviceErr.Payload)
	}
	if payload.DeliveryState != wantDelivery || payload.RequestedRepeats != wantRequested || payload.CompletedRepeats != wantCompleted || payload.RetryableWith == nil {
		t.Fatalf("send failure payload = %#v", payload)
	}
}

func TestSendCommandRejectsUnsupportedAndUnlearnedCommands(t *testing.T) {
	codeService := newCaptureTestService(t, models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"})
	nativeCode := models.IRExecutionModeNativeCode
	_, err := codeService.SendCommand("room", "device", "code", models.IRCommandSendRequest{ExecutionMode: &nativeCode})
	assertServiceErrorMessage(t, err, "native code execution is not available")

	service := newCaptureTestService(t, models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatRawSamples})
	_, err = service.SendCommand("room", "device", "power", models.IRCommandSendRequest{})
	assertServiceErrorMessage(t, err, "ir command is not learned")
	assertServiceErrorKind(t, err, ErrorConflict)

	service = newCaptureTestService(t, models.IRCommand{ID: "preset", Name: "Preset", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_STATUS_CH3"})
	_, err = service.SendCommand("room", "device", "preset", models.IRCommandSendRequest{})
	assertServiceErrorMessage(t, err, "preset command is not allowed")
}

func TestSendCommandRejectsInvalidRequestRepeatAndControllerError(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "on", Name: "On", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
	zero := 0
	_, err := service.SendCommand("room", "device", "on", models.IRCommandSendRequest{Repeat: &zero})
	assertServiceErrorMessage(t, err, "repeat must be between 1 and 5")

	service.sendPresetCommand = func(command string) (string, error) { return "", errors.New("socket closed") }
	one := 1
	_, err = service.SendCommand("room", "device", "on", models.IRCommandSendRequest{Repeat: &one})
	assertServiceErrorMessage(t, err, "controller daemon error")
	assertServiceErrorKind(t, err, ErrorController)
}

func TestSendCommandRejectsExecutionModeForNonCodeBeforeSending(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "on", Name: "On", Format: models.OperationFormatPresetCommand, PicoCommand: "LIGHT_ON_CH3"})
	sendCalls := 0
	service.sendPresetCommand = func(string) (string, error) {
		sendCalls++
		return "ok", nil
	}
	backendRaw := models.IRExecutionModeBackendRaw

	_, err := service.SendCommand("room", "device", "on", models.IRCommandSendRequest{ExecutionMode: &backendRaw})
	assertServiceErrorMessage(t, err, "invalid ir execution mode")
	if sendCalls != 0 {
		t.Fatalf("send called %d times, want 0", sendCalls)
	}
}

func TestSendCommandRejectsUnsafeStoredRawSamplesBeforeSending(t *testing.T) {
	command := models.IRCommand{
		ID:        "power",
		Name:      "Power",
		Format:    models.OperationFormatRawSamples,
		Count:     1,
		TimingsUS: []uint{backendir.MaxRawTimingUS + 1},
	}
	service := newCaptureTestService(t, command)
	sendCalls := 0
	service.sendRaw = func([]uint) (string, error) {
		sendCalls++
		return "ok", nil
	}

	_, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{})
	assertServiceErrorMessage(t, err, "raw timing exceeds maximum")
	if sendCalls != 0 {
		t.Fatalf("send called %d times, want 0", sendCalls)
	}
}

func assertStateSendRepeat(t *testing.T, service IRAdapterService, stateID string, request models.IRStateSendRequest, wantRepeat int, callCount *int) {
	t.Helper()
	*callCount = 0
	response, err := service.SendState("room", "device", stateID, request)
	if err != nil {
		t.Fatalf("send state: %v", err)
	}
	if response.Repeat != wantRepeat || *callCount != wantRepeat {
		t.Fatalf("repeat = (%d, %d calls), want %d", response.Repeat, *callCount, wantRepeat)
	}
}

func confirmedNativeCapabilities() models.IRCapabilities {
	return models.IRCapabilities{OK: true, Commands: []string{"IR_SEND_RAW", "IR_SEND_CODE"}, CodeProtocols: []string{"PANASONIC_40"}, RawCarrierKHz: []int{38}}
}

func TestNativeCodeAvailabilityRequiresCurrentCapabilityEvidence(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"})
	tests := []struct {
		name       string
		capability models.IRCapabilities
		err        error
		available  bool
		status     models.IRVerificationStatus
	}{
		{name: "confirmed", capability: confirmedNativeCapabilities(), available: true, status: models.IRVerificationStatusConfirmed},
		{name: "raw carrier is not a native condition", capability: models.IRCapabilities{OK: true, Commands: []string{"IR_SEND_CODE"}, CodeProtocols: []string{"PANASONIC_40"}}, available: true, status: models.IRVerificationStatusConfirmed},
		{name: "capability failure", err: errors.New("reconnected"), status: models.IRVerificationStatusUnknown},
		{name: "daemon route unconfirmed", capability: models.IRCapabilities{OK: true, CodeProtocols: []string{"PANASONIC_40"}}, status: models.IRVerificationStatusConfirmed},
		{name: "protocol missing", capability: models.IRCapabilities{OK: true, Commands: []string{"IR_SEND_CODE"}}, status: models.IRVerificationStatusConfirmed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
				return test.capability, controller_client.CommandResult{}, test.err
			}
			options, err := service.GetExecutionOptions("room", "device", "PANASONIC_40")
			if err != nil {
				t.Fatalf("GetExecutionOptions: %v", err)
			}
			option := options.ExecutionModes.NativeCode
			if option.Available != test.available || option.VerificationStatus != test.status || (!option.Available && option.Reason == "") {
				t.Fatalf("native option = %#v", option)
			}
		})
	}
}

func TestNativeCodeAvailabilityIsNotCachedAcrossConnectionChanges(t *testing.T) {
	service := newCaptureTestService(t, models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"})
	responses := []models.IRCapabilities{
		confirmedNativeCapabilities(),
		{OK: true, Commands: []string{"IR_SEND_CODE"}},
		{OK: true, CodeProtocols: []string{"PANASONIC_40"}},
	}
	calls := 0
	service.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
		response := responses[calls]
		calls++
		return response, controller_client.CommandResult{}, nil
	}
	for index, wantAvailable := range []bool{true, false, false} {
		options, err := service.GetExecutionOptions("room", "device", "PANASONIC_40")
		if err != nil || options.ExecutionModes.NativeCode.Available != wantAvailable {
			t.Fatalf("call %d options = %#v, %v", index, options, err)
		}
	}
	if calls != 3 {
		t.Fatalf("capability calls = %d, want 3", calls)
	}
}

func TestNativeCodeSendRechecksCapabilityAndDoesNotFallback(t *testing.T) {
	command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C", Repeat: 2}
	service := newCaptureTestService(t, command)
	capabilityCalls := 0
	service.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
		capabilityCalls++
		if capabilityCalls == 1 {
			return confirmedNativeCapabilities(), controller_client.CommandResult{}, nil
		}
		return models.IRCapabilities{}, controller_client.CommandResult{}, errors.New("connection changed")
	}
	options, err := service.GetExecutionOptions("room", "device", command.Protocol)
	if err != nil || !options.ExecutionModes.NativeCode.Available {
		t.Fatalf("options = %#v, %v", options, err)
	}
	rawCalls, codeCalls := 0, 0
	service.sendRawResult = func([]uint) (controller_client.CommandResult, error) {
		rawCalls++
		return controller_client.CommandResult{}, nil
	}
	service.sendCodeResult = func(string, string) (controller_client.CommandResult, error) {
		codeCalls++
		return controller_client.CommandResult{}, nil
	}
	native := models.IRExecutionModeNativeCode
	_, err = service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &native})
	assertServiceErrorKind(t, err, ErrorConflict)
	if rawCalls != 0 || codeCalls != 0 || capabilityCalls != 2 {
		t.Fatalf("calls capability/raw/code = %d/%d/%d", capabilityCalls, rawCalls, codeCalls)
	}
}

func TestNativeCodeCommandAndStateUseBackendRepeat(t *testing.T) {
	native := models.IRExecutionModeNativeCode
	command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C", Repeat: 2}
	service := newCaptureTestService(t, command)
	service.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
		return confirmedNativeCapabilities(), controller_client.CommandResult{}, nil
	}
	commands := []string{}
	service.sendCodeResult = func(protocol, code string) (controller_client.CommandResult, error) {
		commands = append(commands, "IR_SEND_CODE:"+protocol+":"+code)
		return controller_client.CommandResult{ResponseBody: `{"ok":true}`}, nil
	}
	response, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &native})
	if err != nil || response.DeliveryState != models.IRDeliveryStateSent || response.CompletedRepeats != 2 || len(commands) != 2 {
		t.Fatalf("command response/calls = %#v / %#v / %v", response, commands, err)
	}

	state := models.IRState{ID: "on", Name: "On", State: map[string]interface{}{"power": "on"}, Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C"}
	stateService := newStateCaptureTestService(t, state)
	stateService.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
		return confirmedNativeCapabilities(), controller_client.CommandResult{}, nil
	}
	stateCalls := 0
	stateService.sendCodeResult = func(protocol, code string) (controller_client.CommandResult, error) {
		stateCalls++
		return controller_client.CommandResult{}, nil
	}
	stateResponse, err := stateService.SendState("room", "device", state.ID, models.IRStateSendRequest{ExecutionMode: &native})
	if err != nil || stateResponse.CompletedRepeats != 1 || stateCalls != 1 {
		t.Fatalf("state response/calls = %#v / %d / %v", stateResponse, stateCalls, err)
	}
}

func TestNativeCodeTypedFailuresPreserveDeliveryState(t *testing.T) {
	native := models.IRExecutionModeNativeCode
	command := models.IRCommand{ID: "code", Name: "Code", Format: models.OperationFormatCode, Protocol: "PANASONIC_40", Code: "344A90BC2C", Repeat: 2}
	for _, test := range []struct {
		name    string
		failure controller_client.CommandResult
		want    models.IRDeliveryState
	}{
		{name: "pre-send after one success", failure: controller_client.CommandResult{FailureStage: controller_client.FailureStageRemoteReject, RemoteResponse: &controller_client.RemoteResponse{ErrorCode: models.IRWireErrorCodeUnsupportedProtocol}, Err: controller_client.ErrRemoteRejected}, want: models.IRDeliveryStatePartiallySent},
		{name: "read timeout", failure: controller_client.CommandResult{FailureStage: controller_client.FailureStageRead, WriteCompleted: true, Err: errors.New("timeout")}, want: models.IRDeliveryStateUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newCaptureTestService(t, command)
			service.getCapabilities = func() (models.IRCapabilities, controller_client.CommandResult, error) {
				return confirmedNativeCapabilities(), controller_client.CommandResult{}, nil
			}
			calls := 0
			service.sendCodeResult = func(string, string) (controller_client.CommandResult, error) {
				calls++
				if calls == 1 && test.want == models.IRDeliveryStatePartiallySent {
					return controller_client.CommandResult{}, nil
				}
				return test.failure, test.failure.Err
			}
			_, err := service.SendCommand("room", "device", command.ID, models.IRCommandSendRequest{ExecutionMode: &native})
			wantCompleted := 0
			if test.want == models.IRDeliveryStatePartiallySent {
				wantCompleted = 1
			}
			assertTypedSendFailure(t, err, test.want, 2, wantCompleted)
		})
	}
}

func newCaptureTestService(t *testing.T, command models.IRCommand) IRAdapterService {
	t.Helper()
	roomsDir := t.TempDir()
	roomRepository := repositories.NewRoomRepositoryWithBaseDir(roomsDir)
	deviceRepository := repositories.NewDeviceRepositoryWithRoomsDir(roomsDir)
	irAdapterRepository := repositories.NewIRAdapterRepositoryWithRoomsDir(roomsDir)

	if err := roomRepository.Save(models.Room{ID: "room", Name: "Room"}); err != nil {
		t.Fatalf("save room: %v", err)
	}
	if err := deviceRepository.Save(models.Device{ID: "device", Name: "Device", RoomID: "room", DeviceType: "light", PrimaryAdapter: "ir"}); err != nil {
		t.Fatalf("save device: %v", err)
	}
	if err := irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := irAdapterRepository.SaveCommand("room", "device", command); err != nil {
		t.Fatalf("save command: %v", err)
	}

	return IRAdapterService{
		irAdapterRepository: irAdapterRepository,
		deviceRepository:    deviceRepository,
		roomRepository:      roomRepository,
		getCapabilities: func() (models.IRCapabilities, controller_client.CommandResult, error) {
			return models.IRCapabilities{}, controller_client.CommandResult{}, errors.New("capability unavailable")
		},
	}
}

func newStateCaptureTestService(t *testing.T, state models.IRState) IRAdapterService {
	t.Helper()
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	schema := models.IRStateSchema{
		"power": {Type: models.IRStateFieldTypeEnum, Values: []string{"on", "off"}},
	}
	if _, err := service.SaveStateSchema("room", "device", schema); err != nil {
		t.Fatalf("save state schema: %v", err)
	}
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	return service
}

func newStateTestService(t *testing.T, remoteType models.IRRemoteType, saveConfig bool) IRAdapterService {
	t.Helper()
	base := t.TempDir()
	dataRoot := filepath.Join(base, "runtime")
	roomsDir := filepath.Join(dataRoot, "rooms")
	stateRoot := filepath.Join(base, "state")
	trashRoot := filepath.Join(base, "trash")
	for _, dir := range []string{dataRoot, roomsDir, stateRoot, filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), trashRoot} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: stateRoot, RuntimeDataDir: dataRoot, TrashDir: trashRoot})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := repositories.NewTrashRepository(repositories.TrashConfig{Dir: trashRoot, RuntimeDataDir: dataRoot, StateDir: stateRoot, RetentionDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	deleteService := NewCRUDTrashDeleteService(state.Operations(), trash, dataRoot, trashRoot)
	roomRepository := repositories.NewRoomRepositoryWithBaseDir(roomsDir)
	deviceRepository := repositories.NewDeviceRepositoryWithRoomsDir(roomsDir)
	irAdapterRepository := repositories.NewIRAdapterRepositoryWithRoomsDir(roomsDir)

	if err := roomRepository.Save(models.Room{ID: "room", Name: "Room"}); err != nil {
		t.Fatalf("save room: %v", err)
	}
	if err := deviceRepository.Save(models.Device{ID: "device", Name: "Device", RoomID: "room", DeviceType: "air_conditioner", PrimaryAdapter: "ir"}); err != nil {
		t.Fatalf("save device: %v", err)
	}
	if saveConfig {
		if err := irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: remoteType, CarrierKHz: 38}); err != nil {
			t.Fatalf("save config: %v", err)
		}
	}

	return IRAdapterService{
		irAdapterRepository: irAdapterRepository,
		deviceRepository:    deviceRepository,
		roomRepository:      roomRepository,
		trashDeletes:        deleteService,
		getCapabilities: func() (models.IRCapabilities, controller_client.CommandResult, error) {
			return models.IRCapabilities{}, controller_client.CommandResult{}, errors.New("capability unavailable")
		},
	}
}

func irStateWriteRequestFromState(state models.IRState) models.IRStateWriteRequest {
	request := models.IRStateWriteRequest{
		ID:     stringPointer(state.ID),
		Name:   stringPointer(state.Name),
		State:  statePointer(state.State),
		Format: operationFormatPointer(state.Format),
	}
	if state.CarrierKHz != 0 {
		request.CarrierKHz = intPointer(state.CarrierKHz)
	}
	if state.Format != models.OperationFormatRawSamples {
		if state.Count != 0 {
			request.Count = intPointer(state.Count)
		}
		if state.TimingsUS != nil {
			timings := append([]uint(nil), state.TimingsUS...)
			request.TimingsUS = &timings
		}
		if state.Captures != nil {
			captures := append([]models.IRCapture(nil), state.Captures...)
			request.Captures = &captures
		}
	}
	if state.PicoCommand != "" {
		request.PicoCommand = stringPointer(state.PicoCommand)
	}
	if state.Protocol != "" {
		request.Protocol = stringPointer(state.Protocol)
	}
	if state.Code != "" {
		request.Code = stringPointer(state.Code)
	}
	if state.Repeat != 0 {
		request.Repeat = intPointer(state.Repeat)
	}
	return request
}

func stringPointer(value string) *string {
	return &value
}

func intPointer(value int) *int {
	return &value
}

func statePointer(value map[string]interface{}) *map[string]interface{} {
	return &value
}

func operationFormatPointer(value models.OperationFormat) *models.OperationFormat {
	return &value
}

func assertServiceErrorMessage(t *testing.T, err error, want string) {
	t.Helper()
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Message != want {
		t.Fatalf("error = %v, want service error %q", err, want)
	}
}

func assertServiceErrorKind(t *testing.T, err error, want ErrorKind) {
	t.Helper()
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Kind != want {
		t.Fatalf("error = %v, want service error kind %q", err, want)
	}
}
