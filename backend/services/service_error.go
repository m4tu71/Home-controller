package services

import "errors"

const defaultCarrierKHz = 38

type ErrorKind string

const (
	ErrorInvalidRequest      ErrorKind = "invalid_request"
	ErrorNotFound            ErrorKind = "not_found"
	ErrorInternal            ErrorKind = "internal"
	ErrorController          ErrorKind = "controller"
	ErrorCaptureInsufficient ErrorKind = "capture_insufficient"
	ErrorConflict            ErrorKind = "conflict"
)

type ServiceError struct {
	Kind    ErrorKind
	Message string
	Detail  string
	Payload interface{}
	Err     error
}

func (e *ServiceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func AsServiceError(err error) (*ServiceError, bool) {
	var serviceError *ServiceError
	if errors.As(err, &serviceError) {
		return serviceError, true
	}
	return nil, false
}
