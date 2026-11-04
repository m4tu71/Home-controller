package handlers

import (
	"net/http"

	"backend/models"
	"backend/services"
)

var irAdapterService = services.NewIRAdapterService()
var createIRCommandService = irAdapterService.CreateCommand
var createIRStateService = irAdapterService.CreateState
var captureIRCommandService = irAdapterService.CaptureCommand
var sendIRCommandService = irAdapterService.SendCommand
var captureIRStateService = irAdapterService.CaptureState
var sendIRStateService = irAdapterService.SendState
var getIRExecutionOptionsService = irAdapterService.GetExecutionOptions
var deleteIRCommandService = irAdapterService.DeleteCommand
var deleteIRStateService = irAdapterService.DeleteState

func handleIRAdapter(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, parts []string) {
	if len(parts) == 1 && parts[0] == "execution-options" {
		if r.Method != http.MethodGet {
			writeIRMethodNotAllowed(w, "GET")
			return
		}
		getIRExecutionOptions(w, r, roomID, deviceID)
		return
	}

	if len(parts) == 1 && parts[0] == "config" {
		switch r.Method {
		case http.MethodGet:
			getIRConfig(w, r, roomID, deviceID)
		case http.MethodPut:
			saveIRConfig(w, r, roomID, deviceID)
		default:
			writeIRMethodNotAllowed(w, "GET, PUT")
		}
		return
	}

	if len(parts) >= 1 && parts[0] == "commands" {
		handleIRCommands(w, r, roomID, deviceID, parts[1:])
		return
	}

	if len(parts) == 1 && parts[0] == "state-schema" {
		switch r.Method {
		case http.MethodGet:
			getIRStateSchema(w, r, roomID, deviceID)
		case http.MethodPut:
			saveIRStateSchema(w, r, roomID, deviceID)
		default:
			writeIRMethodNotAllowed(w, "GET, PUT")
		}
		return
	}

	if len(parts) >= 1 && parts[0] == "states" {
		handleIRStates(w, r, roomID, deviceID, parts[1:])
		return
	}

	writeJSONError(w, http.StatusNotFound, "route not found")
}

func getIRExecutionOptions(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	options, err := getIRExecutionOptionsService(roomID, deviceID, r.URL.Query().Get("protocol"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, options)
}

func handleIRCommands(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			listIRCommands(w, r, roomID, deviceID)
		case http.MethodPost:
			createIRCommand(w, r, roomID, deviceID)
		default:
			writeIRMethodNotAllowed(w, "GET, POST")
		}
		return
	}

	if len(parts) == 2 && parts[0] != "" {
		switch parts[1] {
		case "capture":
			if r.Method != http.MethodPost {
				writeIRMethodNotAllowed(w, "POST")
				return
			}
			captureIRCommand(w, r, roomID, deviceID, parts[0])
			return
		case "send":
			if r.Method != http.MethodPost {
				writeIRMethodNotAllowed(w, "POST")
				return
			}
			sendIRCommand(w, r, roomID, deviceID, parts[0])
			return
		}
	}

	if len(parts) != 1 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "route not found")
		return
	}

	commandID := parts[0]

	switch r.Method {
	case http.MethodGet:
		getIRCommand(w, r, roomID, deviceID, commandID)
	case http.MethodPut:
		updateIRCommand(w, r, roomID, deviceID, commandID)
	case http.MethodDelete:
		deleteIRCommand(w, r, roomID, deviceID, commandID)
	default:
		writeIRMethodNotAllowed(w, "GET, PUT, DELETE")
	}
}

func handleIRStates(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			listIRStates(w, r, roomID, deviceID)
		case http.MethodPost:
			createIRState(w, r, roomID, deviceID)
		default:
			writeIRMethodNotAllowed(w, "GET, POST")
		}
		return
	}

	if len(parts) == 2 && parts[0] != "" {
		switch parts[1] {
		case "capture":
			if r.Method != http.MethodPost {
				writeIRMethodNotAllowed(w, "POST")
				return
			}
			captureIRState(w, r, roomID, deviceID, parts[0])
			return
		case "send":
			if r.Method != http.MethodPost {
				writeIRMethodNotAllowed(w, "POST")
				return
			}
			sendIRState(w, r, roomID, deviceID, parts[0])
			return
		}
	}

	if len(parts) != 1 || parts[0] == "" {
		writeJSONError(w, http.StatusNotFound, "route not found")
		return
	}

	stateID := parts[0]

	switch r.Method {
	case http.MethodGet:
		getIRState(w, r, roomID, deviceID, stateID)
	case http.MethodPut:
		updateIRState(w, r, roomID, deviceID, stateID)
	case http.MethodDelete:
		deleteIRState(w, r, roomID, deviceID, stateID)
	default:
		writeIRMethodNotAllowed(w, "GET, PUT, DELETE")
	}
}

func getIRConfig(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	config, err := irAdapterService.GetConfig(roomID, deviceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, config)
}

func saveIRConfig(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	config, ok := decodeIRRequest[models.IRConfig](w, r, writeIRRequestError)
	if !ok {
		return
	}

	savedConfig, err := irAdapterService.SaveConfig(roomID, deviceID, config)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, savedConfig)
}

func listIRCommands(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	commands, err := irAdapterService.ListCommands(roomID, deviceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, commands)
}

func createIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	request, ok := decodeIRRequest[models.IRCommandWriteRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	createdCommand, err := createIRCommandService(roomID, deviceID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createdCommand)
}

func getIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, commandID string) {
	command, err := irAdapterService.GetCommand(roomID, deviceID, commandID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, command)
}

func updateIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, commandID string) {
	request, ok := decodeIRRequest[models.IRCommandWriteRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	updatedCommand, err := irAdapterService.UpdateCommand(roomID, deviceID, commandID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updatedCommand)
}

func deleteIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, commandID string) {
	result, err := deleteIRCommandService(roomID, deviceID, commandID)
	if err != nil {
		writeCRUDDeleteError(w, err, result)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"room_id":        roomID,
		"device_id":      deviceID,
		"command_id":     commandID,
		"trash_entry_id": result.TrashEntryID,
		"operation_id":   result.OperationID,
		"status":         result.Status,
	})
}

func captureIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, commandID string) {
	request, ok := decodeIRRequest[models.IRCommandCaptureRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	response, err := captureIRCommandService(roomID, deviceID, commandID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func sendIRCommand(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, commandID string) {
	request, ok := decodeIRSendRequest[models.IRCommandSendRequest](w, r)
	if !ok {
		return
	}

	response, err := sendIRCommandService(roomID, deviceID, commandID, request)
	if err != nil {
		writeIRSendServiceError(w, err, request.ExecutionMode)
		return
	}

	writeIRSendSuccess(w, response)
}

func getIRStateSchema(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	schema, err := irAdapterService.GetStateSchema(roomID, deviceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, schema)
}

func saveIRStateSchema(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	schema, ok := decodeIRRequest[models.IRStateSchema](w, r, writeIRRequestError)
	if !ok {
		return
	}

	savedSchema, err := irAdapterService.SaveStateSchema(roomID, deviceID, schema)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, savedSchema)
}

func listIRStates(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	states, err := irAdapterService.ListStates(roomID, deviceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, states)
}

func createIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string) {
	request, ok := decodeIRRequest[models.IRStateWriteRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	createdState, err := createIRStateService(roomID, deviceID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createdState)
}

func getIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, stateID string) {
	state, err := irAdapterService.GetState(roomID, deviceID, stateID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, state)
}

func updateIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, stateID string) {
	request, ok := decodeIRRequest[models.IRStateWriteRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	updatedState, err := irAdapterService.UpdateState(roomID, deviceID, stateID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updatedState)
}

func deleteIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, stateID string) {
	result, err := deleteIRStateService(roomID, deviceID, stateID)
	if err != nil {
		writeCRUDDeleteError(w, err, result)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"room_id":        roomID,
		"device_id":      deviceID,
		"state_id":       stateID,
		"trash_entry_id": result.TrashEntryID,
		"operation_id":   result.OperationID,
		"status":         result.Status,
	})
}

func captureIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, stateID string) {
	request, ok := decodeIRRequest[models.IRStateCaptureRequest](w, r, writeIRRequestError)
	if !ok {
		return
	}

	response, err := captureIRStateService(roomID, deviceID, stateID, request)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func sendIRState(w http.ResponseWriter, r *http.Request, roomID string, deviceID string, stateID string) {
	request, ok := decodeIRSendRequest[models.IRStateSendRequest](w, r)
	if !ok {
		return
	}

	response, err := sendIRStateService(roomID, deviceID, stateID, request)
	if err != nil {
		writeIRSendServiceError(w, err, request.ExecutionMode)
		return
	}

	writeIRSendSuccess(w, response)
}
