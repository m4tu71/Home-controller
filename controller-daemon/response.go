package main

import "encoding/json"

type ErrorResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code,omitempty"`
}

func makeErrorResponse(message string) string {
	data, err := json.Marshal(ErrorResponse{
		OK:    false,
		Error: message,
	})
	if err != nil {
		return `{"ok":false,"error":"failed to create error response"}`
	}

	return string(data)
}

func makeCodedErrorResponse(message string, errorCode string) string {
	data, err := json.Marshal(ErrorResponse{
		OK:        false,
		Error:     message,
		ErrorCode: errorCode,
	})
	if err != nil {
		return `{"ok":false,"error":"failed to create error response"}`
	}

	return string(data)
}
