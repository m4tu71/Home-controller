package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type maintenanceInputTestRequest struct {
	Value string `json:"value"`
}

func (r maintenanceInputTestRequest) Validate() error {
	if strings.TrimSpace(r.Value) == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}

func TestDecodeMaintenanceRequestStrictJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
		wantError   string
	}{
		{
			name:        "accepts JSON media type with parameters",
			body:        `{"value":"ok"}`,
			contentType: "application/json; charset=utf-8",
			wantStatus:  http.StatusOK,
		},
		{
			name:       "missing content type",
			body:       `{"value":"ok"}`,
			wantStatus: http.StatusUnsupportedMediaType,
			wantError:  "Content-Type must be application/json",
		},
		{
			name:        "wrong content type",
			body:        `{"value":"ok"}`,
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantError:   "Content-Type must be application/json",
		},
		{
			name:        "malformed JSON",
			body:        `{"value":`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid request body",
		},
		{
			name:        "unknown field",
			body:        `{"value":"ok","extra":true}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid request body",
		},
		{
			name:        "second JSON value",
			body:        `{"value":"ok"} {"value":"extra"}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid request body",
		},
		{
			name:        "non-JSON trailing data",
			body:        `{"value":"ok"}x`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid request body",
		},
		{
			name:        "trailing whitespace after one JSON value",
			body:        "{\"value\":\"ok\"} \n\t",
			contentType: "application/json",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "required field validation",
			body:        `{}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
			wantError:   "value is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}

			_, ok := DecodeMaintenanceRequest[maintenanceInputTestRequest](
				recorder,
				request,
				MaintenanceJSONBodyLimit256KiB,
			)
			if !ok && recorder.Code == http.StatusOK {
				t.Fatal("request rejected without an HTTP error")
			}

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantError != "" {
				assertMaintenanceError(t, recorder, test.wantError)
			}
		})
	}
}

func TestDecodeMaintenanceRequestBodyLimitBoundaries(t *testing.T) {
	for _, limit := range []int64{
		MaintenanceJSONBodyLimit256KiB,
		MaintenanceJSONBodyLimit1MiB,
	} {
		t.Run(fmt.Sprintf("limit_%d_bytes", limit), func(t *testing.T) {
			prefix := `{"value":"`
			suffix := `"}`
			paddingSize := int(limit) - len(prefix) - len(suffix)
			exactLimitBody := prefix + strings.Repeat("a", paddingSize) + suffix

			if int64(len(exactLimitBody)) != limit {
				t.Fatalf("test body size = %d, want %d", len(exactLimitBody), limit)
			}

			exactLimitRecorder := decodeTestBody(exactLimitBody, "application/json", limit)
			if exactLimitRecorder.Code != http.StatusOK {
				t.Fatalf("exact-limit status = %d, want %d; body=%s", exactLimitRecorder.Code, http.StatusOK, exactLimitRecorder.Body.String())
			}

			overLimitRecorder := decodeTestBody(exactLimitBody+" ", "application/json", limit)
			if overLimitRecorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("over-limit status = %d, want %d; body=%s", overLimitRecorder.Code, http.StatusRequestEntityTooLarge, overLimitRecorder.Body.String())
			}
			assertMaintenanceError(t, overLimitRecorder, "request body too large")
		})
	}
}

func TestRequireEmptyMaintenanceBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		maxBytes   int64
		content    string
		chunked    bool
		wantStatus int
		wantError  string
	}{
		{
			name:       "empty body ignores content type",
			content:    "text/plain",
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			wantStatus: http.StatusOK,
		},
		{
			name:       "whitespace is not empty",
			body:       " \n",
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			wantStatus: http.StatusBadRequest,
			wantError:  "request body must be empty",
		},
		{
			name:       "empty object is not empty",
			body:       `{}`,
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			wantStatus: http.StatusBadRequest,
			wantError:  "request body must be empty",
		},
		{
			name:       "chunked non-empty body is rejected",
			body:       `{}`,
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			chunked:    true,
			wantStatus: http.StatusBadRequest,
			wantError:  "request body must be empty",
		},
		{
			name:       "at-limit non-empty body is bad request",
			body:       strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)),
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			wantStatus: http.StatusBadRequest,
			wantError:  "request body must be empty",
		},
		{
			name:       "over-limit body is too large",
			body:       strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1),
			maxBytes:   MaintenanceJSONBodyLimit256KiB,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  "request body too large",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.content)
			if test.chunked {
				request.TransferEncoding = []string{"chunked"}
				request.ContentLength = -1
			}

			if !RequireEmptyMaintenanceBody(recorder, request, test.maxBytes) && recorder.Code == http.StatusOK {
				t.Fatal("request rejected without an HTTP error")
			}
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantError != "" {
				assertMaintenanceError(t, recorder, test.wantError)
			}
		})
	}
}

func TestMaintenanceRequestModelsRejectMissingOrUnsafeFields(t *testing.T) {
	backupCases := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "missing plan id", body: `{"plan_digest":"sha256:x","action_ids":["a"]}`, wantError: "plan_id is required"},
		{name: "missing digest", body: `{"plan_id":"plan-a","action_ids":["a"]}`, wantError: "plan_digest is required"},
		{name: "missing actions", body: `{"plan_id":"plan-a","plan_digest":"sha256:x"}`, wantError: "action_ids must contain at least one action"},
		{name: "empty actions", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":[]}`, wantError: "action_ids must contain at least one action"},
		{name: "empty action id", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":[" "]}`, wantError: "action_ids must not contain empty values"},
		{name: "duplicate action id", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":["a","a"]}`, wantError: "action_ids must not contain duplicates"},
		{name: "arbitrary path rejected", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":["a"],"path":"/tmp/file"}`, wantError: "invalid request body"},
		{name: "role rejected", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":["a"],"role":"apply"}`, wantError: "invalid request body"},
		{name: "confirm field rejected", body: `{"plan_id":"plan-a","plan_digest":"sha256:x","action_ids":["a"],"confirm":true}`, wantError: "invalid request body"},
	}

	for _, test := range backupCases {
		t.Run("backup_"+test.name, func(t *testing.T) {
			recorder := decodeBackupRequest(test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			assertMaintenanceError(t, recorder, test.wantError)
		})
	}

	applyCases := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "missing confirm", body: `{"plan_id":"p","plan_digest":"d","action_ids":["a"]}`, wantError: "confirm must be true"},
		{name: "false confirm", body: `{"plan_id":"p","plan_digest":"d","action_ids":["a"],"confirm":false}`, wantError: "confirm must be true"},
		{name: "no selected action", body: `{"plan_id":"p","plan_digest":"d","action_ids":[],"confirm":true}`, wantError: "action_ids must contain at least one action"},
	}
	for _, test := range applyCases {
		t.Run("apply_"+test.name, func(t *testing.T) {
			recorder := decodeApplyRequest(test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			assertMaintenanceError(t, recorder, test.wantError)
		})
	}

	trashCases := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "missing inventory hash", body: `{"confirm":true}`, wantError: "expected_inventory_hash is required"},
		{name: "missing confirm", body: `{"expected_inventory_hash":"sha256:x"}`, wantError: "confirm must be true"},
		{name: "false confirm", body: `{"expected_inventory_hash":"sha256:x","confirm":false}`, wantError: "confirm must be true"},
		{name: "arbitrary restore path rejected", body: `{"expected_inventory_hash":"sha256:x","confirm":true,"path":"/tmp/restore"}`, wantError: "invalid request body"},
	}
	for _, test := range trashCases {
		t.Run("trash_restore_"+test.name, func(t *testing.T) {
			recorder := decodeTrashRestoreRequest(test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			assertMaintenanceError(t, recorder, test.wantError)
		})
	}
}

func decodeTestBody(body, contentType string, limit int64) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	_, _ = DecodeMaintenanceRequest[maintenanceInputTestRequest](recorder, request, limit)
	return recorder
}

func decodeBackupRequest(body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	_, _ = DecodeMaintenanceRequest[models.BackupCreateRequest](
		recorder,
		request,
		MaintenanceJSONBodyLimit256KiB,
	)
	return recorder
}

func decodeApplyRequest(body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	_, _ = DecodeMaintenanceRequest[models.MaintenanceApplyRequest](
		recorder,
		request,
		MaintenanceJSONBodyLimit1MiB,
	)
	return recorder
}

func decodeTrashRestoreRequest(body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	_, _ = DecodeMaintenanceRequest[models.TrashRestoreRequest](
		recorder,
		request,
		MaintenanceJSONBodyLimit256KiB,
	)
	return recorder
}

func assertMaintenanceError(t *testing.T, recorder *httptest.ResponseRecorder, wantError string) {
	t.Helper()

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var response models.MaintenanceErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.OK {
		t.Fatal("error response ok = true, want false")
	}
	if response.Error != wantError {
		t.Fatalf("error = %q, want %q", response.Error, wantError)
	}
}
