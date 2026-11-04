package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"backend/models"
)

const irAdapterJSONBodyLimit int64 = 1024 * 1024

type irRequestErrorWriter func(http.ResponseWriter, int, string)

func decodeIRRequest[T any](w http.ResponseWriter, r *http.Request, writeError irRequestErrorWriter) (T, bool) {
	var request T
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	limited := http.MaxBytesReader(w, body, irAdapterJSONBodyLimit)
	data, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return request, false
	}
	if !hasJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return request, false
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}
	return request, true
}

func writeIRRequestError(w http.ResponseWriter, status int, message string) {
	writeJSONError(w, status, message)
}

func decodeIRSendRequest[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	return decodeIRRequest[T](w, r, func(w http.ResponseWriter, status int, message string) {
		writeIRSendErrorResponse(w, status, models.IRSendErrorResponse{
			OK:            false,
			Error:         message,
			DeliveryState: models.IRDeliveryStateNotSent,
			RetryableWith: []models.IRExecutionMode{},
		})
	})
}

func writeIRMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}
