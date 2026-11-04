package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/models"
	"backend/services"
)

func TestIRExecutionOptionsRouteReturnsAvailability(t *testing.T) {
	original := getIRExecutionOptionsService
	t.Cleanup(func() { getIRExecutionOptionsService = original })
	getIRExecutionOptionsService = func(roomID string, deviceID string, protocol string) (models.IRExecutionOptionsResponse, error) {
		if roomID != "room" || deviceID != "device" || protocol != "PANASONIC_40" {
			t.Fatalf("execution options request = (%q, %q, %q)", roomID, deviceID, protocol)
		}
		return models.IRExecutionOptionsResponse{
			Format:   models.OperationFormatCode,
			Protocol: protocol,
			ExecutionModes: models.IRExecutionModeOptions{
				NativeCode: models.IRExecutionModeOption{Available: false, VerificationStatus: models.IRVerificationStatusUnknown, Reason: "PicoのPANASONIC_40対応を確認できません"},
				BackendRaw: models.IRExecutionModeOption{Available: true, VerificationStatus: models.IRVerificationStatusConfirmed, Reason: ""},
			},
		}, nil
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/controller/rooms/room/devices/device/ir/execution-options?protocol=PANASONIC_40", nil)
	handleIRAdapter(recorder, request, "room", "device", []string{"execution-options"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response models.IRExecutionOptionsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Format != models.OperationFormatCode || response.Protocol != "PANASONIC_40" || response.ExecutionModes.NativeCode.Available || response.ExecutionModes.NativeCode.Reason == "" || !response.ExecutionModes.BackendRaw.Available {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRExecutionOptionsRouteRejectsMissingProtocolAndUnsupportedMethod(t *testing.T) {
	t.Run("missing protocol", func(t *testing.T) {
		original := getIRExecutionOptionsService
		t.Cleanup(func() { getIRExecutionOptionsService = original })
		getIRExecutionOptionsService = func(string, string, string) (models.IRExecutionOptionsResponse, error) {
			return models.IRExecutionOptionsResponse{}, &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "ir code protocol is not supported"}
		}

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/controller/rooms/room/devices/device/ir/execution-options", nil)
		handleIRAdapter(recorder, request, "room", "device", []string{"execution-options"})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/controller/rooms/room/devices/device/ir/execution-options?protocol=PANASONIC_40", nil)
		handleIRAdapter(recorder, request, "room", "device", []string{"execution-options"})
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestIRCommandCaptureRouteRejectsUnsupportedMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/controller/rooms/room/devices/device/ir/commands/power/capture", nil)

	handleIRCommands(recorder, request, "room", "device", []string{"power", "capture"})

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestIRCommandSendRouteRejectsUnsupportedMethod(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/controller/rooms/room/devices/device/ir/commands/power/send", nil)

	handleIRCommands(recorder, request, "room", "device", []string{"power", "send"})

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestIRCommandCaptureRouteRejectsUnknownSubroute(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/controller/rooms/room/devices/device/ir/commands/power/unknown", nil)

	handleIRCommands(recorder, request, "room", "device", []string{"power", "unknown"})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestIRStateCaptureRouteReturnsCaptureResponse(t *testing.T) {
	originalCaptureState := captureIRStateService
	t.Cleanup(func() { captureIRStateService = originalCaptureState })
	captureIRStateService = func(roomID string, deviceID string, stateID string, request models.IRStateCaptureRequest) (models.IRStateCaptureResponse, error) {
		if roomID != "room" || deviceID != "device" || stateID != "cool_26_auto" {
			t.Fatalf("capture target = (%q, %q, %q)", roomID, deviceID, stateID)
		}
		if request.Samples == nil || *request.Samples != 3 || request.MaxAttempts == nil || *request.MaxAttempts != 9 {
			t.Fatalf("capture request = %#v", request)
		}
		return models.IRStateCaptureResponse{
			OK:              true,
			State:           models.IRState{ID: stateID, Format: models.OperationFormatRawSamples},
			RequiredSamples: 3,
			ValidCaptures:   3,
		}, nil
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{"samples":3,"max_attempts":9}`)

	handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "capture"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response models.IRStateCaptureResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK || response.State.ID != "cool_26_auto" || response.RequiredSamples != 3 || response.ValidCaptures != 3 {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRStateCaptureRouteReturnsInsufficientCapturePayload(t *testing.T) {
	originalCaptureState := captureIRStateService
	t.Cleanup(func() { captureIRStateService = originalCaptureState })
	captureIRStateService = func(string, string, string, models.IRStateCaptureRequest) (models.IRStateCaptureResponse, error) {
		return models.IRStateCaptureResponse{}, &services.ServiceError{
			Kind:    services.ErrorCaptureInsufficient,
			Message: "not enough valid captures",
			Payload: map[string]interface{}{
				"ok":                 false,
				"error":              "not enough valid captures",
				"required_samples":   3,
				"valid_captures":     1,
				"count_distribution": map[string]int{"4": 1},
				"captures":           []models.IRCapture{{Attempt: 1, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}}},
			},
		}
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{}`)

	handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "capture"})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	var response map[string]interface{}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["error"] != "not enough valid captures" || response["required_samples"] != float64(3) || response["valid_captures"] != float64(1) || response["count_distribution"] == nil || response["captures"] == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRStateCaptureRouteRejectsInvalidMethodAndSubroute(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		parts      []string
		wantStatus int
	}{
		{name: "unsupported method", method: http.MethodGet, parts: []string{"cool_26_auto", "capture"}, wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown subroute", method: http.MethodPost, parts: []string{"cool_26_auto", "unknown"}, wantStatus: http.StatusNotFound},
		{name: "invalid state id", method: http.MethodPost, parts: []string{"", "capture"}, wantStatus: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, "/api/controller/rooms/room/devices/device/ir/states/cool_26_auto/capture", nil)

			handleIRStates(recorder, request, "room", "device", test.parts)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func TestIRStateCaptureRouteRejectsInvalidRequestBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{`)

	handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "capture"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestIRStateSendRouteReturnsSendResponse(t *testing.T) {
	originalSendState := sendIRStateService
	t.Cleanup(func() { sendIRStateService = originalSendState })
	sendIRStateService = func(roomID string, deviceID string, stateID string, request models.IRStateSendRequest) (models.IRStateSendResponse, error) {
		if roomID != "room" || deviceID != "device" || stateID != "cool_26_auto" {
			t.Fatalf("send target = (%q, %q, %q)", roomID, deviceID, stateID)
		}
		if request.Repeat == nil || *request.Repeat != 3 {
			t.Fatalf("send request = %#v", request)
		}
		return models.IRStateSendResponse{
			OK:               true,
			RoomID:           roomID,
			DeviceID:         deviceID,
			StateID:          stateID,
			Format:           models.OperationFormatRawSamples,
			DeliveryState:    models.IRDeliveryStateSent,
			Repeat:           3,
			RequestedRepeats: 3,
			CompletedRepeats: 3,
			RetryableWith:    []models.IRExecutionMode{},
		}, nil
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{"repeat":3}`)

	handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "send"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response models.IRStateSendResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK || response.RoomID != "room" || response.DeviceID != "device" || response.StateID != "cool_26_auto" || response.Format != models.OperationFormatRawSamples || response.DeliveryState != models.IRDeliveryStateSent || response.Repeat != 3 || response.RequestedRepeats != 3 || response.CompletedRepeats != 3 || response.RetryableWith == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRCommandSendRouteReturnsSendResponse(t *testing.T) {
	originalSendCommand := sendIRCommandService
	t.Cleanup(func() { sendIRCommandService = originalSendCommand })
	sendIRCommandService = func(roomID string, deviceID string, commandID string, request models.IRCommandSendRequest) (models.IRCommandSendResponse, error) {
		if roomID != "room" || deviceID != "device" || commandID != "power" {
			t.Fatalf("send target = (%q, %q, %q)", roomID, deviceID, commandID)
		}
		if request.Repeat == nil || *request.Repeat != 3 || request.ExecutionMode != nil {
			t.Fatalf("send request = %#v", request)
		}
		return models.IRCommandSendResponse{
			OK:               true,
			RoomID:           roomID,
			DeviceID:         deviceID,
			CommandID:        commandID,
			Format:           models.OperationFormatRawSamples,
			DeliveryState:    models.IRDeliveryStateSent,
			Repeat:           3,
			RequestedRepeats: 3,
			CompletedRepeats: 3,
			RetryableWith:    []models.IRExecutionMode{},
		}, nil
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{"repeat":3}`)
	handleIRCommands(recorder, request, "room", "device", []string{"power", "send"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response models.IRCommandSendResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK || response.RoomID != "room" || response.DeviceID != "device" || response.CommandID != "power" || response.Format != models.OperationFormatRawSamples || response.DeliveryState != models.IRDeliveryStateSent || response.Repeat != 3 || response.RequestedRepeats != 3 || response.CompletedRepeats != 3 || response.RetryableWith == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRCommandSendRouteReturnsFixedExecutionModeErrorPayload(t *testing.T) {
	originalSendCommand := sendIRCommandService
	t.Cleanup(func() { sendIRCommandService = originalSendCommand })
	sendIRCommandService = func(string, string, string, models.IRCommandSendRequest) (models.IRCommandSendResponse, error) {
		return models.IRCommandSendResponse{}, &services.ServiceError{
			Kind:    services.ErrorInvalidRequest,
			Message: "invalid ir execution mode",
			Payload: models.IRSendErrorResponse{
				OK:            false,
				Error:         "invalid ir execution mode",
				ExecutionMode: models.IRExecutionModeBackendRaw,
				DeliveryState: models.IRDeliveryStateNotSent,
				RetryableWith: []models.IRExecutionMode{},
			},
		}
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{"execution_mode":"backend_raw"}`)
	handleIRCommands(recorder, request, "room", "device", []string{"power", "send"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response models.IRSendErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OK || response.Error != "invalid ir execution mode" || response.ExecutionMode != models.IRExecutionModeBackendRaw || response.DeliveryState != models.IRDeliveryStateNotSent || response.RetryableWith == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRStateSendRouteReturnsFixedExecutionModeErrorPayload(t *testing.T) {
	originalSendState := sendIRStateService
	t.Cleanup(func() { sendIRStateService = originalSendState })
	sendIRStateService = func(string, string, string, models.IRStateSendRequest) (models.IRStateSendResponse, error) {
		return models.IRStateSendResponse{}, &services.ServiceError{
			Kind:    services.ErrorInvalidRequest,
			Message: "invalid ir execution mode",
			Payload: models.IRSendErrorResponse{
				OK:            false,
				Error:         "invalid ir execution mode",
				ExecutionMode: models.IRExecutionModeBackendRaw,
				DeliveryState: models.IRDeliveryStateNotSent,
				RetryableWith: []models.IRExecutionMode{},
			},
		}
	}

	recorder := httptest.NewRecorder()
	request := newIRAdapterTestRequest(http.MethodPost, `{"execution_mode":"backend_raw"}`)
	handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "send"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response models.IRSendErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OK || response.Error != "invalid ir execution mode" || response.ExecutionMode != models.IRExecutionModeBackendRaw || response.DeliveryState != models.IRDeliveryStateNotSent || response.RetryableWith == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestIRStateSendRouteMapsServiceErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		request    string
		serviceErr *services.ServiceError
		wantStatus int
		wantError  string
	}{
		{name: "explicit zero", request: `{"repeat":0}`, serviceErr: &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "repeat must be between 1 and 5"}, wantStatus: http.StatusBadRequest, wantError: "repeat must be between 1 and 5"},
		{name: "not stateful", request: `{}`, serviceErr: &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "ir remote type is not stateful"}, wantStatus: http.StatusBadRequest, wantError: "ir remote type is not stateful"},
		{name: "schema missing", request: `{}`, serviceErr: &services.ServiceError{Kind: services.ErrorNotFound, Message: "ir state schema not found"}, wantStatus: http.StatusNotFound, wantError: "ir state schema not found"},
		{name: "unlearned", request: `{}`, serviceErr: &services.ServiceError{Kind: services.ErrorConflict, Message: "ir state is not learned"}, wantStatus: http.StatusConflict, wantError: "ir state is not learned"},
		{name: "unsupported format", request: `{}`, serviceErr: &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "ir state format is not supported for send"}, wantStatus: http.StatusBadRequest, wantError: "ir state format is not supported for send"},
		{name: "controller error", request: `{}`, serviceErr: &services.ServiceError{Kind: services.ErrorController, Message: "controller daemon error"}, wantStatus: http.StatusInternalServerError, wantError: "controller daemon error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalSendState := sendIRStateService
			t.Cleanup(func() { sendIRStateService = originalSendState })
			sendIRStateService = func(string, string, string, models.IRStateSendRequest) (models.IRStateSendResponse, error) {
				return models.IRStateSendResponse{}, test.serviceErr
			}

			recorder := httptest.NewRecorder()
			request := newIRAdapterTestRequest(http.MethodPost, test.request)

			handleIRStates(recorder, request, "room", "device", []string{"cool_26_auto", "send"})

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var response map[string]interface{}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response["error"] != test.wantError {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestIRStateSendRouteRejectsInvalidMethodSubrouteAndBody(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		body       string
		parts      []string
		wantStatus int
	}{
		{name: "unsupported method", method: http.MethodGet, parts: []string{"cool_26_auto", "send"}, wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown subroute", method: http.MethodPost, parts: []string{"cool_26_auto", "unknown"}, wantStatus: http.StatusNotFound},
		{name: "invalid state id", method: http.MethodPost, parts: []string{"", "send"}, wantStatus: http.StatusNotFound},
		{name: "invalid request body", method: http.MethodPost, body: `{`, parts: []string{"cool_26_auto", "send"}, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := newIRAdapterTestRequest(test.method, test.body)

			handleIRStates(recorder, request, "room", "device", test.parts)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func TestCaptureInsufficientErrorMapsToUnprocessableEntity(t *testing.T) {
	status := serviceErrorStatus(&services.ServiceError{Kind: services.ErrorCaptureInsufficient})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}
}

func TestConflictErrorMapsToConflict(t *testing.T) {
	status := serviceErrorStatus(&services.ServiceError{Kind: services.ErrorConflict})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}
}

func TestIRCommandSendRouteNormalizesAllErrorShapes(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantState  models.IRDeliveryState
		requested  int
		completed  int
	}{
		{name: "validation", serviceErr: &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "repeat must be between 1 and 5"}, wantStatus: http.StatusBadRequest, wantState: models.IRDeliveryStateNotSent},
		{name: "capability", serviceErr: &services.ServiceError{Kind: services.ErrorConflict, Message: "native code execution is not available"}, wantStatus: http.StatusConflict, wantState: models.IRDeliveryStateNotSent},
		{name: "partial", serviceErr: &services.ServiceError{Kind: services.ErrorController, Message: "controller daemon error", Payload: models.IRSendErrorResponse{Error: "controller daemon error", DeliveryState: models.IRDeliveryStatePartiallySent, RequestedRepeats: 3, CompletedRepeats: 1}}, wantStatus: http.StatusInternalServerError, wantState: models.IRDeliveryStatePartiallySent, requested: 3, completed: 1},
		{name: "unknown", serviceErr: &services.ServiceError{Kind: services.ErrorController, Message: "controller daemon error", Payload: models.IRSendErrorResponse{Error: "controller daemon error", DeliveryState: models.IRDeliveryStateUnknown, RequestedRepeats: 3}}, wantStatus: http.StatusInternalServerError, wantState: models.IRDeliveryStateUnknown, requested: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := sendIRCommandService
			t.Cleanup(func() { sendIRCommandService = original })
			sendIRCommandService = func(string, string, string, models.IRCommandSendRequest) (models.IRCommandSendResponse, error) {
				return models.IRCommandSendResponse{}, test.serviceErr
			}
			recorder := httptest.NewRecorder()
			request := newIRAdapterTestRequest(http.MethodPost, `{"execution_mode":"native_code"}`)
			handleIRCommands(recorder, request, "room", "device", []string{"code", "send"})
			assertIRSendErrorResponse(t, recorder, test.wantStatus, test.wantState, test.requested, test.completed)
		})
	}
}

func TestIRStateSendRouteNormalizesPayloadlessAndTypedErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		serviceErr error
		wantStatus int
		wantState  models.IRDeliveryState
	}{
		{name: "validation", serviceErr: &services.ServiceError{Kind: services.ErrorInvalidRequest, Message: "unsupported format"}, wantStatus: http.StatusBadRequest, wantState: models.IRDeliveryStateNotSent},
		{name: "controller", serviceErr: &services.ServiceError{Kind: services.ErrorController, Message: "controller daemon error", Payload: models.IRSendErrorResponse{Error: "controller daemon error", DeliveryState: models.IRDeliveryStateUnknown}}, wantStatus: http.StatusInternalServerError, wantState: models.IRDeliveryStateUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := sendIRStateService
			t.Cleanup(func() { sendIRStateService = original })
			sendIRStateService = func(string, string, string, models.IRStateSendRequest) (models.IRStateSendResponse, error) {
				return models.IRStateSendResponse{}, test.serviceErr
			}
			recorder := httptest.NewRecorder()
			request := newIRAdapterTestRequest(http.MethodPost, `{}`)
			handleIRStates(recorder, request, "room", "device", []string{"state", "send"})
			assertIRSendErrorResponse(t, recorder, test.wantStatus, test.wantState, 0, 0)
		})
	}
}

func TestIRSendRoutesReturnFixedShapeForInvalidJSON(t *testing.T) {
	for _, target := range []string{"command", "state"} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := newIRAdapterTestRequest(http.MethodPost, `{`)
			if target == "command" {
				handleIRCommands(recorder, request, "room", "device", []string{"code", "send"})
			} else {
				handleIRStates(recorder, request, "room", "device", []string{"state", "send"})
			}
			assertIRSendErrorResponse(t, recorder, http.StatusBadRequest, models.IRDeliveryStateNotSent, 0, 0)
		})
	}
}

func assertIRSendErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantState models.IRDeliveryState, wantRequested int, wantCompleted int) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	var response models.IRSendErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OK || response.Error == "" || response.DeliveryState != wantState || response.RequestedRepeats != wantRequested || response.CompletedRepeats != wantCompleted || response.RetryableWith == nil {
		t.Fatalf("response = %#v", response)
	}
}
