package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/services"
)

func newIRAdapterTestRequest(method, body string) *http.Request {
	request := httptest.NewRequest(method, "/ir", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func callIRAdapterRoute(recorder *httptest.ResponseRecorder, request *http.Request, parts []string) {
	handleIRAdapter(recorder, request, "room", "device", parts)
}

func TestIRAdapterWriteBodyContract(t *testing.T) {
	routes := []struct {
		name, method string
		parts        []string
		send         bool
	}{
		{"config update", http.MethodPut, []string{"config"}, false},
		{"command create", http.MethodPost, []string{"commands"}, false},
		{"command update", http.MethodPut, []string{"commands", "power"}, false},
		{"command capture", http.MethodPost, []string{"commands", "power", "capture"}, false},
		{"command send", http.MethodPost, []string{"commands", "power", "send"}, true},
		{"state schema update", http.MethodPut, []string{"state-schema"}, false},
		{"state create", http.MethodPost, []string{"states"}, false},
		{"state update", http.MethodPut, []string{"states", "power"}, false},
		{"state capture", http.MethodPost, []string{"states", "power", "capture"}, false},
		{"state send", http.MethodPost, []string{"states", "power", "send"}, true},
	}
	for _, route := range routes {
		for _, invalid := range []struct {
			name, body, contentType, wantError string
			status                             int
		}{
			{"missing Content-Type", `{}`, "", "Content-Type must be application/json", http.StatusUnsupportedMediaType},
			{"wrong Content-Type", `{}`, "text/plain", "Content-Type must be application/json", http.StatusUnsupportedMediaType},
			{"empty body", "", "application/json", "invalid request body", http.StatusBadRequest},
			{"unknown field or schema value type", `{"unknown":true}`, "application/json", "invalid request body", http.StatusBadRequest},
			{"second JSON value", `{} {}`, "application/json", "invalid request body", http.StatusBadRequest},
			{"malformed JSON", `{`, "application/json", "invalid request body", http.StatusBadRequest},
			{"over limit before media type", strings.Repeat("x", int(irAdapterJSONBodyLimit)+1), "text/plain", "request body too large", http.StatusRequestEntityTooLarge},
		} {
			t.Run(route.name+"/"+invalid.name, func(t *testing.T) {
				request := httptest.NewRequest(route.method, "/ir", strings.NewReader(invalid.body))
				if invalid.contentType != "" {
					request.Header.Set("Content-Type", invalid.contentType)
				}
				recorder := httptest.NewRecorder()
				callIRAdapterRoute(recorder, request, route.parts)
				if recorder.Code != invalid.status {
					t.Fatalf("status = %d, want %d; body=%s", recorder.Code, invalid.status, recorder.Body.String())
				}
				if got := recorder.Header().Get("Content-Type"); got != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", got)
				}
				if route.send {
					var payload models.IRSendErrorResponse
					if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
						t.Fatalf("decode send error: %v", err)
					}
					if payload.OK || payload.Error != invalid.wantError || payload.DeliveryState != models.IRDeliveryStateNotSent || payload.RetryableWith == nil || len(payload.RetryableWith) != 0 {
						t.Fatalf("send error = %#v, want not_sent with no retry alternatives", payload)
					}
					return
				}
				var payload struct {
					OK    bool   `json:"ok"`
					Error string `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode error: %v", err)
				}
				if payload.OK || payload.Error != invalid.wantError {
					t.Fatalf("error body = %#v, want %q", payload, invalid.wantError)
				}
			})
		}
	}
}

func TestIRAdapterCreateReturns201AndDuplicateReturns409(t *testing.T) {
	t.Run("Command", func(t *testing.T) {
		original := createIRCommandService
		t.Cleanup(func() { createIRCommandService = original })
		createIRCommandService = func(roomID, deviceID string, request models.IRCommandWriteRequest) (models.IRCommand, error) {
			if request.ID == nil || *request.ID != "power" {
				t.Fatalf("request = %#v", request)
			}
			return models.IRCommand{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand}, nil
		}
		request := newIRAdapterTestRequest(http.MethodPost, `{"id":"power","name":"Power","format":"preset_command","pico_command":"LIGHT_ON_CH3"}`)
		recorder := httptest.NewRecorder()
		callIRAdapterRoute(recorder, request, []string{"commands"})
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
		}

		createIRCommandService = func(string, string, models.IRCommandWriteRequest) (models.IRCommand, error) {
			return models.IRCommand{}, &services.ServiceError{Kind: services.ErrorConflict, Message: "ir command already exists"}
		}
		request = newIRAdapterTestRequest(http.MethodPost, `{"id":"power","name":"Power","format":"preset_command","pico_command":"LIGHT_ON_CH3"}`)
		recorder = httptest.NewRecorder()
		callIRAdapterRoute(recorder, request, []string{"commands"})
		assertIRBasicError(t, recorder, http.StatusConflict, "ir command already exists")
	})

	t.Run("State", func(t *testing.T) {
		original := createIRStateService
		t.Cleanup(func() { createIRStateService = original })
		createIRStateService = func(roomID, deviceID string, request models.IRStateWriteRequest) (models.IRState, error) {
			if request.ID == nil || *request.ID != "power" {
				t.Fatalf("request = %#v", request)
			}
			return models.IRState{ID: "power", Name: "Power", Format: models.OperationFormatPresetCommand}, nil
		}
		request := newIRAdapterTestRequest(http.MethodPost, `{"id":"power","name":"Power","state":{"power":"on"},"format":"preset_command","pico_command":"LIGHT_ON_CH3"}`)
		recorder := httptest.NewRecorder()
		callIRAdapterRoute(recorder, request, []string{"states"})
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create status = %d, want 201; body=%s", recorder.Code, recorder.Body.String())
		}

		createIRStateService = func(string, string, models.IRStateWriteRequest) (models.IRState, error) {
			return models.IRState{}, &services.ServiceError{Kind: services.ErrorConflict, Message: "ir state already exists"}
		}
		request = newIRAdapterTestRequest(http.MethodPost, `{"id":"power","name":"Power","state":{"power":"on"},"format":"preset_command","pico_command":"LIGHT_ON_CH3"}`)
		recorder = httptest.NewRecorder()
		callIRAdapterRoute(recorder, request, []string{"states"})
		assertIRBasicError(t, recorder, http.StatusConflict, "ir state already exists")
	})
}

func TestIRCommandCaptureInsufficientResponseMatchesStateContract(t *testing.T) {
	original := captureIRCommandService
	t.Cleanup(func() { captureIRCommandService = original })
	captureIRCommandService = func(string, string, string, models.IRCommandCaptureRequest) (models.IRCommandCaptureResponse, error) {
		return models.IRCommandCaptureResponse{}, &services.ServiceError{
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
	callIRAdapterRoute(recorder, newIRAdapterTestRequest(http.MethodPost, `{}`), []string{"commands", "power", "capture"})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	var payload struct {
		OK                bool               `json:"ok"`
		Error             string             `json:"error"`
		RequiredSamples   int                `json:"required_samples"`
		ValidCaptures     int                `json:"valid_captures"`
		CountDistribution map[string]int     `json:"count_distribution"`
		Captures          []models.IRCapture `json:"captures"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode capture error: %v", err)
	}
	if payload.OK || payload.Error != "not enough valid captures" || payload.RequiredSamples != 3 || payload.ValidCaptures != 1 || payload.CountDistribution["4"] != 1 || len(payload.Captures) != 1 {
		t.Fatalf("capture error = %#v", payload)
	}
}

func TestIRAdapterMethodNotAllowedContract(t *testing.T) {
	for _, test := range []struct {
		name, method, allow string
		parts               []string
	}{
		{"execution options", http.MethodPost, "GET", []string{"execution-options"}},
		{"config", http.MethodPost, "GET, PUT", []string{"config"}},
		{"state schema", http.MethodPost, "GET, PUT", []string{"state-schema"}},
		{"command collection", http.MethodDelete, "GET, POST", []string{"commands"}},
		{"command item", http.MethodPost, "GET, PUT, DELETE", []string{"commands", "power"}},
		{"command capture", http.MethodGet, "POST", []string{"commands", "power", "capture"}},
		{"command send", http.MethodGet, "POST", []string{"commands", "power", "send"}},
		{"state collection", http.MethodDelete, "GET, POST", []string{"states"}},
		{"state item", http.MethodPost, "GET, PUT, DELETE", []string{"states", "power"}},
		{"state capture", http.MethodGet, "POST", []string{"states", "power", "capture"}},
		{"state send", http.MethodGet, "POST", []string{"states", "power", "send"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			callIRAdapterRoute(recorder, httptest.NewRequest(test.method, "/ir", nil), test.parts)
			assertIRBasicError(t, recorder, http.StatusMethodNotAllowed, "method not allowed")
			if got := recorder.Header().Get("Allow"); got != test.allow {
				t.Fatalf("Allow = %q, want %q", got, test.allow)
			}
		})
	}
}

func TestDecodeIRRequestBodyLimitBoundaries(t *testing.T) {
	type typedRequest struct {
		Value int `json:"value"`
	}
	request := httptest.NewRequest(http.MethodPost, "/ir", strings.NewReader("{\"value\":1} \n"))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	recorder := httptest.NewRecorder()
	if _, ok := decodeIRRequest[typedRequest](recorder, request, writeIRRequestError); !ok {
		t.Fatalf("parameterized Content-Type or trailing whitespace rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = newIRAdapterTestRequest(http.MethodPost, `{"value":"wrong type"}`)
	if _, ok := decodeIRRequest[typedRequest](recorder, request, writeIRRequestError); ok {
		t.Fatal("field type mismatch accepted")
	}
	assertIRBasicError(t, recorder, http.StatusBadRequest, "invalid request body")

	requestBody := `{}` + strings.Repeat(" ", int(irAdapterJSONBodyLimit)-2)
	recorder = httptest.NewRecorder()
	request = newIRAdapterTestRequest(http.MethodPost, requestBody)
	if _, ok := decodeIRRequest[map[string]any](recorder, request, writeIRRequestError); !ok {
		t.Fatalf("exact limit rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("exact limit status = %d, want 200", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	request = newIRAdapterTestRequest(http.MethodPost, requestBody+" ")
	if _, ok := decodeIRRequest[map[string]any](recorder, request, writeIRRequestError); ok {
		t.Fatal("over-limit body accepted")
	}
	assertIRBasicError(t, recorder, http.StatusRequestEntityTooLarge, "request body too large")
}

func assertIRBasicError(t *testing.T, recorder *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, status, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload.OK || payload.Error != message {
		t.Fatalf("error body = %#v, want %q", payload, message)
	}
}
