package controller_client

import (
	"bufio"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/models"
)

type stubConn struct {
	writeN      int
	writeErr    error
	readData    []byte
	readErr     error
	deadlineErr error
	deadline    time.Time
}

func (conn *stubConn) Read(buffer []byte) (int, error) {
	if len(conn.readData) > 0 {
		n := copy(buffer, conn.readData)
		conn.readData = conn.readData[n:]
		if len(conn.readData) == 0 && conn.readErr != nil {
			return n, conn.readErr
		}
		return n, nil
	}
	if conn.readErr != nil {
		return 0, conn.readErr
	}
	return 0, io.EOF
}

func (conn *stubConn) Write(buffer []byte) (int, error) {
	if conn.writeN < 0 {
		return len(buffer), conn.writeErr
	}
	return conn.writeN, conn.writeErr
}

func (conn *stubConn) Close() error         { return nil }
func (conn *stubConn) LocalAddr() net.Addr  { return stubAddr("local") }
func (conn *stubConn) RemoteAddr() net.Addr { return stubAddr("remote") }
func (conn *stubConn) SetDeadline(deadline time.Time) error {
	conn.deadline = deadline
	return conn.deadlineErr
}
func (conn *stubConn) SetReadDeadline(time.Time) error  { return nil }
func (conn *stubConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr string

func (addr stubAddr) Network() string { return "stub" }
func (addr stubAddr) String() string  { return string(addr) }

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func dialStub(conn net.Conn, err error) dialTimeoutFunc {
	return func(string, string, time.Duration) (net.Conn, error) {
		return conn, err
	}
}

func TestSendCommandResultUsesFifteenSecondSocketBound(t *testing.T) {
	conn := &stubConn{writeN: -1, readData: []byte("{\"ok\":true}\n")}
	start := time.Now()
	var dialTimeout time.Duration
	result, err := sendCommandResult("IR_CAPTURE", func(_, _ string, timeout time.Duration) (net.Conn, error) {
		dialTimeout = timeout
		return conn, nil
	})
	if err != nil || result.FailureStage != "" {
		t.Fatalf("capture response: %+v, %v", result, err)
	}
	if dialTimeout != 15*time.Second {
		t.Fatalf("dial timeout = %v", dialTimeout)
	}
	if conn.deadline.Before(start.Add(15*time.Second)) || conn.deadline.After(time.Now().Add(15*time.Second)) {
		t.Fatalf("connection deadline = %v; expected about 15s after connect", conn.deadline)
	}
}

func TestSendCommandResultTransportFailures(t *testing.T) {
	tests := []struct {
		name               string
		dial               dialTimeoutFunc
		wantStage          FailureStage
		wantBytesWritten   int
		wantWriteCompleted bool
		wantResponse       string
	}{
		{
			name:      "connect failure",
			dial:      dialStub(nil, errors.New("connect failed")),
			wantStage: FailureStageConnect,
		},
		{
			name:      "zero byte write failure",
			dial:      dialStub(&stubConn{writeN: 0, writeErr: errors.New("write failed")}, nil),
			wantStage: FailureStageWrite,
		},
		{
			name:             "partial write timeout",
			dial:             dialStub(&stubConn{writeN: 3, writeErr: timeoutError{}}, nil),
			wantStage:        FailureStageWrite,
			wantBytesWritten: 3,
		},
		{
			name:               "full write timeout",
			dial:               dialStub(&stubConn{writeN: -1, writeErr: timeoutError{}}, nil),
			wantStage:          FailureStageWrite,
			wantBytesWritten:   len("IR_TEST\n"),
			wantWriteCompleted: true,
		},
		{
			name:               "full write then read timeout",
			dial:               dialStub(&stubConn{writeN: -1, readErr: timeoutError{}}, nil),
			wantStage:          FailureStageRead,
			wantBytesWritten:   len("IR_TEST\n"),
			wantWriteCompleted: true,
		},
		{
			name:               "read failure",
			dial:               dialStub(&stubConn{writeN: -1, readErr: errors.New("read failed")}, nil),
			wantStage:          FailureStageRead,
			wantBytesWritten:   len("IR_TEST\n"),
			wantWriteCompleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sendCommandResult("IR_TEST", tt.dial)
			if err == nil {
				t.Fatal("sendCommandResult() error = nil, want error")
			}
			if result.Err == nil {
				t.Fatal("CommandResult.Err = nil, want underlying error")
			}
			if result.FailureStage != tt.wantStage {
				t.Fatalf("FailureStage = %q, want %q", result.FailureStage, tt.wantStage)
			}
			if result.BytesWritten != tt.wantBytesWritten {
				t.Fatalf("BytesWritten = %d, want %d", result.BytesWritten, tt.wantBytesWritten)
			}
			if result.WriteCompleted != tt.wantWriteCompleted {
				t.Fatalf("WriteCompleted = %t, want %t", result.WriteCompleted, tt.wantWriteCompleted)
			}
			if result.ResponseBody != tt.wantResponse {
				t.Fatalf("ResponseBody = %q, want %q", result.ResponseBody, tt.wantResponse)
			}
		})
	}
}

func TestSendCommandResultResponses(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantStage FailureStage
		wantErr   error
		wantOK    bool
		wantError string
		wantCode  models.IRWireErrorCode
	}{
		{
			name:      "invalid JSON",
			response:  "not-json\n",
			wantStage: FailureStageResponseParse,
			wantErr:   ErrInvalidResponse,
		},
		{
			name:      "remote rejection",
			response:  `{"ok":false,"error":"unsupported protocol","error_code":"unsupported_protocol"}` + "\n",
			wantStage: FailureStageRemoteReject,
			wantErr:   ErrRemoteRejected,
			wantError: "unsupported protocol",
			wantCode:  models.IRWireErrorCodeUnsupportedProtocol,
		},
		{
			name:     "success",
			response: `{"ok":true}` + "\n",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sendCommandResult("IR_TEST", dialStub(&stubConn{writeN: -1, readData: []byte(tt.response)}, nil))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("sendCommandResult() error = %v, want %v", err, tt.wantErr)
			}
			if result.FailureStage != tt.wantStage {
				t.Fatalf("FailureStage = %q, want %q", result.FailureStage, tt.wantStage)
			}
			if result.ResponseBody != tt.response {
				t.Fatalf("ResponseBody = %q, want %q", result.ResponseBody, tt.response)
			}
			if tt.wantStage == FailureStageResponseParse {
				if result.RemoteResponse != nil {
					t.Fatalf("RemoteResponse = %#v, want nil", result.RemoteResponse)
				}
				return
			}
			if result.RemoteResponse == nil {
				t.Fatal("RemoteResponse = nil, want parsed response")
			}
			if result.RemoteResponse.OK != tt.wantOK || result.RemoteResponse.Error != tt.wantError || result.RemoteResponse.ErrorCode != tt.wantCode {
				t.Fatalf("RemoteResponse = %#v", result.RemoteResponse)
			}
		})
	}
}

func TestCommandResultIsPreSendRejectionAllowlist(t *testing.T) {
	tests := []struct {
		name     string
		code     models.IRWireErrorCode
		response string
		want     bool
	}{
		{name: "unsupported command", code: models.IRWireErrorCodeUnsupportedCommand, want: true},
		{name: "unsupported protocol", code: models.IRWireErrorCodeUnsupportedProtocol, want: true},
		{name: "invalid code", code: models.IRWireErrorCodeInvalidCode, want: true},
		{name: "invalid raw timing", code: models.IRWireErrorCodeInvalidRawTiming, want: true},
		{name: "invalid raw separator", code: models.IRWireErrorCodeInvalidRawSeparator, want: true},
		{name: "empty raw timings", code: models.IRWireErrorCodeEmptyRawTimings, want: true},
		{name: "raw timing exceeds maximum", code: models.IRWireErrorCodeRawTimingExceedsMaximum, want: true},
		{name: "raw duration exceeds maximum", code: models.IRWireErrorCodeRawDurationExceedsMaximum, want: true},
		{name: "too many raw timings", code: models.IRWireErrorCodeTooManyRawTimings, want: true},
		{name: "missing", response: `{"ok":false,"error":"rejected"}` + "\n", want: false},
		{name: "unknown", code: "unknown_code", want: false},
		{name: "internal error", code: "internal_error", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := tt.response
			if response == "" {
				response = `{"ok":false,"error":"rejected","error_code":"` + string(tt.code) + `"}` + "\n"
			}
			result, err := sendCommandResult("IR_TEST", dialStub(&stubConn{
				writeN:   -1,
				readData: []byte(response),
			}, nil))
			if !errors.Is(err, ErrRemoteRejected) {
				t.Fatalf("sendCommandResult() error = %v, want remote rejection", err)
			}
			if got := result.IsPreSendRejection(); got != tt.want {
				t.Fatalf("IsPreSendRejection() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestConfiguredSocketPathConnectsToUnixListener(t *testing.T) {
	legacyPath := currentSocketPath()
	t.Cleanup(func() { _ = ConfigureSocketPath(legacyPath) })
	path := filepath.Join(t.TempDir(), "controller.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := ConfigureSocketPath(path); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			accepted <- acceptErr
			return
		}
		defer connection.Close()
		command, readErr := bufio.NewReader(connection).ReadString('\n')
		if readErr != nil {
			accepted <- readErr
			return
		}
		if strings.TrimSpace(command) != "LED_STATUS" {
			accepted <- errors.New("unexpected command")
			return
		}
		_, writeErr := connection.Write([]byte("{\"ok\":true}\n"))
		accepted <- writeErr
	}()
	result, err := SendCommandResult("LED_STATUS")
	if err != nil || result.RemoteResponse == nil || !result.RemoteResponse.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestConfigureSocketPathRejectsRelativeAndNULPaths(t *testing.T) {
	for _, path := range []string{"relative.sock", "/tmp/controller\x00.sock"} {
		if err := ConfigureSocketPath(path); err == nil {
			t.Fatalf("ConfigureSocketPath(%q) unexpectedly succeeded", path)
		}
	}
}

func TestConfiguredSocketConnectionFailureIsClassified(t *testing.T) {
	legacyPath := currentSocketPath()
	t.Cleanup(func() { _ = ConfigureSocketPath(legacyPath) })
	path := filepath.Join(t.TempDir(), "missing.sock")
	if err := ConfigureSocketPath(path); err != nil {
		t.Fatal(err)
	}
	result, err := SendCommandResult("LED_STATUS")
	if err == nil || result.FailureStage != FailureStageConnect {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCommandResultTransportFailureIsNotPreSendRemoteRejection(t *testing.T) {
	result := CommandResult{
		FailureStage: FailureStageRead,
		RemoteResponse: &RemoteResponse{
			ErrorCode: models.IRWireErrorCodeUnsupportedProtocol,
		},
	}
	if result.IsPreSendRejection() {
		t.Fatal("read failure must remain unknown even when a wire code is present")
	}
}
