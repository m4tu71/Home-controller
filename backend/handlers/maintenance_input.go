package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"backend/models"
)

const (
	// MaintenanceJSONBodyLimit256KiB applies to migration verify, cleanup,
	// Trash, and body-bearing backup POST requests.
	MaintenanceJSONBodyLimit256KiB int64 = 256 * 1024
	// MaintenanceJSONBodyLimit1MiB applies to migration dry-run, apply, and
	// rollback requests.
	MaintenanceJSONBodyLimit1MiB int64 = 1024 * 1024
)

// MaintenanceRequest is implemented by typed maintenance request models that
// perform their endpoint-level required-field checks after strict decoding.
type MaintenanceRequest interface {
	Validate() error
}

// DecodeMaintenanceRequest reads a bounded JSON body, requires the JSON media
// type, rejects unknown fields and additional JSON values, and validates the
// decoded request before returning it to a handler.
func DecodeMaintenanceRequest[T MaintenanceRequest](
	w http.ResponseWriter,
	r *http.Request,
	maxBytes int64,
) (T, bool) {
	var request T

	body, err := readMaintenanceBody(w, r, maxBytes)
	if err != nil {
		writeMaintenanceBodyReadError(w, err)
		return request, false
	}

	if !hasJSONContentType(r.Header.Get("Content-Type")) {
		writeMaintenanceInputError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return request, false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}

	var trailingValue json.RawMessage
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid request body")
		return request, false
	}

	if err := request.Validate(); err != nil {
		writeMaintenanceInputError(w, http.StatusBadRequest, err.Error())
		return request, false
	}

	return request, true
}

// RequireEmptyMaintenanceBody accepts only a zero-byte body. Content-Type is
// intentionally ignored for endpoints whose contract has no request body.
func RequireEmptyMaintenanceBody(
	w http.ResponseWriter,
	r *http.Request,
	maxBytes int64,
) bool {
	body, err := readMaintenanceBody(w, r, maxBytes)
	if err != nil {
		writeMaintenanceBodyReadError(w, err)
		return false
	}
	if len(body) != 0 {
		writeMaintenanceInputError(w, http.StatusBadRequest, "request body must be empty")
		return false
	}
	return true
}

func readMaintenanceBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errInvalidMaintenanceBodyLimit
	}

	body := r.Body
	if body == nil {
		body = http.NoBody
	}

	limitedBody := http.MaxBytesReader(w, body, maxBytes)
	data, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func hasJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func writeMaintenanceBodyReadError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeMaintenanceInputError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	if errors.Is(err, errInvalidMaintenanceBodyLimit) {
		writeMaintenanceInputError(w, http.StatusInternalServerError, "invalid maintenance request body limit")
		return
	}
	writeMaintenanceInputError(w, http.StatusBadRequest, "invalid request body")
}

func writeMaintenanceInputError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(models.MaintenanceErrorResponse{
		OK:    false,
		Error: message,
	})
}

var errInvalidMaintenanceBodyLimit = errors.New("invalid maintenance request body limit")
