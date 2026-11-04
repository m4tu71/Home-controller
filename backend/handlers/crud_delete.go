package handlers

import (
	"errors"
	"net/http"

	"backend/repositories"
	"backend/services"
)

var configuredRoomsDir string

// ConfigureDataRoot wires ordinary Room/Device/IR operations to the selected
// runtime data root. An empty value restores the historical default used by
// tests and direct package callers.
func ConfigureDataRoot(roomsDir string) {
	configuredRoomsDir = roomsDir
	configureServices(nil)
}

func configureServices(deletes *services.CRUDTrashDeleteService) {
	if configuredRoomsDir == "" {
		roomService = services.NewRoomService().WithTrashDeletes(deletes)
		deviceService = services.NewDeviceService().WithTrashDeletes(deletes)
		irAdapterService = services.NewIRAdapterService().WithTrashDeletes(deletes)
	} else {
		roomService = services.NewRoomServiceWithBaseDir(configuredRoomsDir).WithTrashDeletes(deletes)
		deviceService = services.NewDeviceServiceWithRoomsDir(configuredRoomsDir).WithTrashDeletes(deletes)
		irAdapterService = services.NewIRAdapterServiceWithRoomsDir(configuredRoomsDir).WithTrashDeletes(deletes)
	}
	createIRCommandService = irAdapterService.CreateCommand
	createIRStateService = irAdapterService.CreateState
	captureIRCommandService = irAdapterService.CaptureCommand
	sendIRCommandService = irAdapterService.SendCommand
	captureIRStateService = irAdapterService.CaptureState
	sendIRStateService = irAdapterService.SendState
	getIRExecutionOptionsService = irAdapterService.GetExecutionOptions
	deleteIRCommandService = irAdapterService.DeleteCommand
	deleteIRStateService = irAdapterService.DeleteState
}

// ConfigureCRUDTrashDeleteService wires ordinary resource DELETE routes to the
// same durable operation and Trash boundary used by Maintenance operations.
// A nil service intentionally leaves deletion unavailable (fail closed).
func ConfigureCRUDTrashDeleteService(deletes *services.CRUDTrashDeleteService) {
	configureServices(deletes)
}

func writeCRUDDeleteError(w http.ResponseWriter, err error, result services.CRUDDeleteResult) {
	if result.OperationID != "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":             false,
			"error":          "delete outcome may be partial; inspect operation and Trash before retrying",
			"operation_id":   result.OperationID,
			"trash_entry_id": result.TrashEntryID,
			"status":         result.Status,
		})
		return
	}
	if errors.Is(err, repositories.ErrMaintenanceTargetClaimed) || errors.Is(err, repositories.ErrMaintenanceDuplicate) || errors.Is(err, services.ErrCRUDDeleteTargetStale) || errors.Is(err, repositories.ErrTrashChanged) || errors.Is(err, repositories.ErrTrashCollision) {
		writeJSONError(w, http.StatusConflict, "delete target changed or already has an operation; inspect current data before retrying")
		return
	}
	if errors.Is(err, services.ErrMaintenancePlanNotReady) || errors.Is(err, repositories.ErrTrashBoundary) {
		writeJSONError(w, http.StatusServiceUnavailable, "Trash deletion unavailable")
		return
	}
	writeServiceError(w, err)
}
