package handlers

import (
	"net/http"
	"strings"

	"backend/models"
	"backend/services"
)

type roomOperations interface {
	ListRooms() ([]models.Room, error)
	CreateRoom(models.Room) (models.Room, error)
	GetRoom(string) (models.Room, error)
	UpdateRoom(string, models.Room) (models.Room, error)
	DeleteRoom(string) (services.CRUDDeleteResult, error)
}

var roomService roomOperations = services.NewRoomService()

func RoomsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/controller/rooms")
	path = strings.Trim(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			listRooms(w, r)
		case http.MethodPost:
			createRoom(w, r)
		default:
			writeRoomDeviceMethodNotAllowed(w, "GET, POST")
		}
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[1] == "devices" {
		handleDevices(w, r, parts[0], parts[2:])
		return
	}

	if len(parts) != 1 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "route not found")
		return
	}

	roomID := parts[0]

	switch r.Method {
	case http.MethodGet:
		getRoom(w, r, roomID)
	case http.MethodPut:
		updateRoom(w, r, roomID)
	case http.MethodDelete:
		deleteRoom(w, r, roomID)
	default:
		writeRoomDeviceMethodNotAllowed(w, "GET, PUT, DELETE")
	}
}

func listRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := roomService.ListRooms()
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, rooms)
}

func createRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := decodeRoomDeviceRequest[models.Room](w, r)
	if !ok {
		return
	}

	createdRoom, err := roomService.CreateRoom(room)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createdRoom)
}

func getRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	room, err := roomService.GetRoom(roomID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, room)
}

func updateRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	room, ok := decodeRoomDeviceRequest[models.Room](w, r)
	if !ok {
		return
	}

	updatedRoom, err := roomService.UpdateRoom(roomID, room)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updatedRoom)
}

func deleteRoom(w http.ResponseWriter, r *http.Request, roomID string) {
	result, err := roomService.DeleteRoom(roomID)
	if err != nil {
		writeCRUDDeleteError(w, err, result)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"room_id":        roomID,
		"trash_entry_id": result.TrashEntryID,
		"operation_id":   result.OperationID,
		"status":         result.Status,
	})
}
