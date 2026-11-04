package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

type fakePicoResponsePort struct {
	readTimeout   time.Duration
	responseDelay time.Duration
	response      []byte
	command       string
	readErr       error
}

func (p *fakePicoResponsePort) Close() error { return nil }

func (p *fakePicoResponsePort) SetReadTimeout(timeout time.Duration) error {
	p.readTimeout = timeout
	return nil
}

func (p *fakePicoResponsePort) Write(data []byte) (int, error) {
	p.command = string(data)
	return len(data), nil
}

func (p *fakePicoResponsePort) Read(data []byte) (int, error) {
	if p.readErr != nil {
		return 0, p.readErr
	}
	if p.responseDelay > p.readTimeout {
		return 0, nil // go.bug.st/serial reports a read timeout this way.
	}
	n := copy(data, p.response)
	p.response = p.response[n:]
	return n, nil
}

func TestReadPicoResponseCaptureTimeoutBoundary(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		delay       time.Duration
		wantTimeout time.Duration
		wantError   bool
	}{
		{"capture after ordinary timeout", "IR_CAPTURE", 5 * time.Second, picoCaptureReadTimeout, false},
		{"capture at boundary", "IR_CAPTURE", picoCaptureReadTimeout, picoCaptureReadTimeout, false},
		{"capture beyond boundary", "IR_CAPTURE", picoCaptureReadTimeout + time.Millisecond, picoCaptureReadTimeout, true},
		{"ordinary at boundary", "IR_TEST", picoReadTimeout, picoReadTimeout, false},
		{"ordinary beyond boundary", "IR_TEST", picoReadTimeout + time.Millisecond, picoReadTimeout, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := &fakePicoResponsePort{responseDelay: tt.delay, response: []byte("{\"ok\":true}\n")}
			response, err := readPicoResponse(port, tt.command)
			if port.readTimeout != tt.wantTimeout || port.command != tt.command+"\n" {
				t.Fatalf("timeout/command: %v, %q", port.readTimeout, port.command)
			}
			if tt.wantError {
				if !errors.Is(err, errPicoReadTimeout) || !strings.Contains(err.Error(), "failed to read response") {
					t.Fatalf("expected classified serial timeout, got %v", err)
				}
				return
			}
			if err != nil || response != `{"ok":true}` {
				t.Fatalf("response=%q, err=%v", response, err)
			}
		})
	}
}

func TestReadPicoResponseReturnsReadFailure(t *testing.T) {
	injected := errors.New("serial disconnected")
	port := &fakePicoResponsePort{readErr: injected}
	_, err := readPicoResponse(port, "IR_CAPTURE")
	if !errors.Is(err, injected) {
		t.Fatalf("expected serial failure, got %v", err)
	}
}

func TestReadPicoResponsePartialLineThenTimeout(t *testing.T) {
	port := &fakePicoResponsePort{response: []byte(`{"ok":true}`)}
	_, err := readPicoResponse(port, "IR_CAPTURE")
	if !errors.Is(err, errPicoReadTimeout) {
		t.Fatalf("partial response must not be accepted: %v", err)
	}
}

func TestSendCommandToPicoUsesConfiguredSerialPath(t *testing.T) {
	port := &fakePicoResponsePort{response: []byte("{\"ok\":true}\n")}
	var gotPath string
	response, err := sendCommandToPicoAt("/dev/serial/by-id/test-pico", "IR_TEST", func(path string, mode *serial.Mode) (picoResponsePort, error) {
		gotPath = path
		if mode.BaudRate != picoBaudRate {
			t.Fatalf("baud rate=%d, want %d", mode.BaudRate, picoBaudRate)
		}
		return port, nil
	})
	if err != nil || response != `{"ok":true}` || gotPath != "/dev/serial/by-id/test-pico" {
		t.Fatalf("response=%q path=%q err=%v", response, gotPath, err)
	}
}
