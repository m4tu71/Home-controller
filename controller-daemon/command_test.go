package main

import (
	"errors"
	"testing"
)

func withPicoForwarder(t *testing.T, forwarder func(string) (string, error)) {
	t.Helper()
	original := forwardCommandToPico
	forwardCommandToPico = forwarder
	t.Cleanup(func() { forwardCommandToPico = original })
}

func TestHandleCommandRoutesIRCapabilitiesAndSendCode(t *testing.T) {
	commands := []string{irCapabilitiesCommand, "IR_SEND_CODE:PANASONIC_40:344A90BC2C"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			calls := 0
			withPicoForwarder(t, func(received string) (string, error) {
				calls++
				if received != command {
					t.Fatalf("forwarded command = %q, want %q", received, command)
				}
				return `{"ok":true}`, nil
			})

			if response := handleCommand(command); response != `{"ok":true}` {
				t.Fatalf("response = %s", response)
			}
			if calls != 1 {
				t.Fatalf("forward calls = %d, want 1", calls)
			}
		})
	}
}

func TestHandleCommandRejectsInvalidIRSendCodeBeforeForwarding(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		response string
	}{
		{name: "missing payload", command: "IR_SEND_CODE:", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "missing code", command: "IR_SEND_CODE:PANASONIC_40:", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "extra separator", command: "IR_SEND_CODE:PANASONIC_40:344A90BC2C:EXTRA", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "unsupported protocol", command: "IR_SEND_CODE:NEC:344A90BC2C", response: `{"ok":false,"error":"unsupported protocol","error_code":"unsupported_protocol"}`},
		{name: "lowercase protocol", command: "IR_SEND_CODE:panasonic_40:344A90BC2C", response: `{"ok":false,"error":"unsupported protocol","error_code":"unsupported_protocol"}`},
		{name: "protocol too long", command: "IR_SEND_CODE:ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFG:344A90BC2C", response: `{"ok":false,"error":"unsupported protocol","error_code":"unsupported_protocol"}`},
		{name: "short code", command: "IR_SEND_CODE:PANASONIC_40:344A90BC2", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "long code", command: "IR_SEND_CODE:PANASONIC_40:344A90BC2C0", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "code exceeds maximum", command: "IR_SEND_CODE:PANASONIC_40:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "lowercase code", command: "IR_SEND_CODE:PANASONIC_40:344a90bc2c", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
		{name: "non hex code", command: "IR_SEND_CODE:PANASONIC_40:344A90BC2Z", response: `{"ok":false,"error":"invalid code","error_code":"invalid_code"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			withPicoForwarder(t, func(string) (string, error) {
				calls++
				return "", nil
			})
			if response := handleCommand(test.command); response != test.response {
				t.Fatalf("response = %s, want %s", response, test.response)
			}
			if calls != 0 {
				t.Fatalf("forward calls = %d, want 0", calls)
			}
		})
	}
}

func TestHandleCommandRejectsUnsupportedIRCommandWithFixedCode(t *testing.T) {
	withPicoForwarder(t, func(string) (string, error) {
		t.Fatal("unsupported IR command must not be forwarded")
		return "", nil
	})
	want := `{"ok":false,"error":"unsupported command","error_code":"unsupported_command"}`
	if response := handleCommand("IR_SEND_CODE"); response != want {
		t.Fatalf("response = %s, want %s", response, want)
	}
}

func TestHandleCommandPreservesPicoResponse(t *testing.T) {
	picoResponse := `{"ok":false,"device":"ir_sender","error":"unsupported protocol","error_code":"unsupported_protocol"}`
	withPicoForwarder(t, func(string) (string, error) { return picoResponse, nil })
	if response := handleCommand("IR_SEND_CODE:PANASONIC_40:344A90BC2C"); response != picoResponse {
		t.Fatalf("response = %s, want exact Pico response %s", response, picoResponse)
	}
}

func TestHandleCommandKeepsTransportErrorsUncoded(t *testing.T) {
	withPicoForwarder(t, func(string) (string, error) {
		return "", errors.New("failed to read response: timeout")
	})
	want := `{"ok":false,"error":"failed to read response: timeout"}`
	if response := handleCommand(irCapabilitiesCommand); response != want {
		t.Fatalf("response = %s, want %s", response, want)
	}
}

func TestHandleCommandKeepsExistingUnknownCommandShape(t *testing.T) {
	withPicoForwarder(t, func(string) (string, error) {
		t.Fatal("unknown command must not be forwarded")
		return "", nil
	})
	if response := handleCommand("ARBITRARY_COMMAND"); response != `{"ok":false,"error":"unknown command"}` {
		t.Fatalf("response = %s", response)
	}
}
