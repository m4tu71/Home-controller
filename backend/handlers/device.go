package handlers

import (
	"net/http"

	"backend/models"
	"backend/services"
)

type deviceOperations interface {
	ListDevices(string) ([]models.Device, error)
	CreateDevice(string, models.Device) (models.Device, error)
	GetDevice(string, string) (models.Device, error)
	UpdateDevice(string, string, models.Device) (models.Device, error)
	DeleteDevice(string, string) (services.CRUDDeleteResult, error)
}

var deviceService deviceOperations = services.NewDeviceService()

func handleDevices(w http.ResponseWriter, r *http.Request, roomID string, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			listDevices(w, r, roomID)
		case http.MethodPost:
			createDevice(w, r, roomID)
		default:
			writeRoomDeviceMethodNotAllowed(w, "GET, POST")
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "ir" {
		handleIRAdapter(w, r, roomID, parts[0], parts[2:])
		return
	}

	if len(parts) != 1 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "route not found")
		return
	}

	deviceID := parts[0]

	switch r.Method {
	case http.MethodGet:
		getDevice(w, r, roomID, deviceID)
	case http.MethodPut:
		updateDevice(w, r, roomID, deviceID)
	case http.MethodDelete:
		deleteDevice(w, r, roomID, deviceID)
	default:
		writeRoomDeviceMethodNotAllowed(w, "GET, PUT, DELETE")
	}
}

func listDevices(w http.ResponseWriter, r *http.Request, roomID string) {
	devices, err := deviceService.ListDevices(roomID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, devices)
}

func createDevice(w http.ResponseWriter, r *http.Request, roomID string) {
	device, ok := decodeRoomDeviceRequest[models.Device](w, r)
	if !ok {
		return
	}

	createdDevice, err := deviceService.CreateDevice(roomID, device)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createdDevice)
}

func getDevice(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	device, err := deviceService.GetDevice(roomID, deviceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, device)
}

func updateDevice(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	device, ok := decodeRoomDeviceRequest[models.Device](w, r)
	if !ok {
		return
	}

	updatedDevice, err := deviceService.UpdateDevice(roomID, deviceID, device)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updatedDevice)
}

func deleteDevice(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	result, err := deviceService.DeleteDevice(roomID, deviceID)
	if err != nil {
		writeCRUDDeleteError(w, err, result)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"room_id":        roomID,
		"device_id":      deviceID,
		"trash_entry_id": result.TrashEntryID,
		"operation_id":   result.OperationID,
		"status":         result.Status,
	})
}
