package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

const roomDeviceJSONBodyLimit int64 = 256 * 1024

func decodeRoomDeviceRequest[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var request T
	body, err := readMaintenanceBody(w, r, roomDeviceJSONBodyLimit)
	if err != nil {
		writeMaintenanceBodyReadError(w, err)
		return request, false
	}
	if !hasJSONContentType(r.Header.Get("Content-Type")) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return request, false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}
	return request, true
}

func writeRoomDeviceMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}
