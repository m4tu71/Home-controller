package main

import (
	"encoding/json"
	"testing"
)

func TestMakeErrorResponseOmitsErrorCode(t *testing.T) {
	response := makeErrorResponse("unknown command")
	if response != `{"ok":false,"error":"unknown command"}` {
		t.Fatalf("response = %s", response)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(response), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, exists := payload["error_code"]; exists {
		t.Fatalf("error_code must be omitted: %#v", payload)
	}
}

func TestMakeCodedErrorResponseIncludesErrorCode(t *testing.T) {
	response := makeCodedErrorResponse("unsupported protocol", "unsupported_protocol")
	want := `{"ok":false,"error":"unsupported protocol","error_code":"unsupported_protocol"}`
	if response != want {
		t.Fatalf("response = %s, want %s", response, want)
	}
}
