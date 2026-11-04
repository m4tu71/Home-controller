package handlers

import (
	"encoding/json"
	"net/http"

	"backend/models"
	"backend/services"
)

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": message})
}

func writeJSONErrorWithDetail(w http.ResponseWriter, status int, message string, detail string) {
	writeJSON(w, status, map[string]interface{}{"ok": false, "error": message, "detail": detail})
}

func writeServiceError(w http.ResponseWriter, err error) {
	serviceError, ok := services.AsServiceError(err)
	if !ok {
		writeJSONErrorWithDetail(w, http.StatusInternalServerError, "internal server error", err.Error())
		return
	}
	if serviceError.Payload != nil {
		writeJSON(w, serviceErrorStatus(serviceError), serviceError.Payload)
		return
	}
	if serviceError.Detail != "" {
		writeJSONErrorWithDetail(w, serviceErrorStatus(serviceError), serviceError.Message, serviceError.Detail)
		return
	}
	writeJSONError(w, serviceErrorStatus(serviceError), serviceError.Message)
}

func writeIRSendSuccess(w http.ResponseWriter, response interface{}) {
	writeJSON(w, http.StatusOK, response)
}

func writeIRSendErrorResponse(w http.ResponseWriter, status int, payload models.IRSendErrorResponse) {
	if payload.RetryableWith == nil {
		payload.RetryableWith = []models.IRExecutionMode{}
	}
	writeJSON(w, status, payload)
}

func writeIRSendServiceError(w http.ResponseWriter, err error, executionMode *models.IRExecutionMode) {
	serviceError, ok := services.AsServiceError(err)
	if !ok {
		writeIRSendErrorResponse(w, http.StatusInternalServerError, models.IRSendErrorResponse{
			OK: false, Error: "internal server error", Detail: err.Error(),
			ExecutionMode: executionModeValue(executionMode), DeliveryState: models.IRDeliveryStateUnknown,
			RetryableWith: []models.IRExecutionMode{},
		})
		return
	}
	if payload, ok := serviceError.Payload.(models.IRSendErrorResponse); ok {
		writeIRSendErrorResponse(w, serviceErrorStatus(serviceError), payload)
		return
	}
	if payload, ok := serviceError.Payload.(*models.IRSendErrorResponse); ok && payload != nil {
		writeIRSendErrorResponse(w, serviceErrorStatus(serviceError), *payload)
		return
	}
	var issues []models.IRStateSchemaIssue
	if payload, ok := serviceError.Payload.(map[string]interface{}); ok {
		issues, _ = payload["issues"].([]models.IRStateSchemaIssue)
	}
	deliveryState := models.IRDeliveryStateNotSent
	if serviceError.Kind == services.ErrorController || serviceError.Kind == services.ErrorInternal {
		deliveryState = models.IRDeliveryStateUnknown
	}
	writeIRSendErrorResponse(w, serviceErrorStatus(serviceError), models.IRSendErrorResponse{
		OK: false, Error: serviceError.Message, Detail: serviceError.Detail, Issues: issues,
		ExecutionMode: executionModeValue(executionMode), DeliveryState: deliveryState,
		RetryableWith: []models.IRExecutionMode{},
	})
}

func executionModeValue(executionMode *models.IRExecutionMode) models.IRExecutionMode {
	if executionMode == nil {
		return ""
	}
	return *executionMode
}

func serviceErrorStatus(err *services.ServiceError) int {
	switch err.Kind {
	case services.ErrorInvalidRequest:
		return http.StatusBadRequest
	case services.ErrorCaptureInsufficient:
		return http.StatusUnprocessableEntity
	case services.ErrorConflict:
		return http.StatusConflict
	case services.ErrorNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
