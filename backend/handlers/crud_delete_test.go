package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/services"
)

func TestIRDeleteRoutesReturnTrashAndOperationIDs(t *testing.T) {
	oldCommand, oldState := deleteIRCommandService, deleteIRStateService
	t.Cleanup(func() { deleteIRCommandService, deleteIRStateService = oldCommand, oldState })
	deleteIRCommandService = func(roomID, deviceID, commandID string) (services.CRUDDeleteResult, error) {
		return services.CRUDDeleteResult{TrashEntryID: "trash_command", OperationID: "delete_command", Status: "verified"}, nil
	}
	deleteIRStateService = func(roomID, deviceID, stateID string) (services.CRUDDeleteResult, error) {
		return services.CRUDDeleteResult{TrashEntryID: "trash_state", OperationID: "delete_state", Status: "verified"}, nil
	}
	for _, tc := range []struct {
		path string
		want string
	}{
		{"commands/power", "trash_command"},
		{"states/cool", "trash_state"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodDelete, "/api/controller/rooms/bedroom/devices/lamp/ir/"+tc.path, nil)
			response := httptest.NewRecorder()
			RoomsHandler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body struct {
				TrashEntryID string `json:"trash_entry_id"`
				OperationID  string `json:"operation_id"`
				Status       string `json:"status"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.TrashEntryID != tc.want || body.OperationID == "" || body.Status != "verified" {
				t.Fatalf("delete body=%+v err=%v", body, err)
			}
		})
	}
}

func TestCRUDDeletePartialFailureReturnsInspectionIDs(t *testing.T) {
	write := httptest.NewRecorder()
	writeCRUDDeleteError(write, errors.New("readback failed"), services.CRUDDeleteResult{
		TrashEntryID: "trash_partial", OperationID: "delete_partial", Status: "verification_failed",
	})
	if write.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", write.Code, write.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(write.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["operation_id"] != "delete_partial" || body["trash_entry_id"] != "trash_partial" || body["status"] != "verification_failed" {
		t.Fatalf("partial outcome IDs were lost: %#v", body)
	}
}
