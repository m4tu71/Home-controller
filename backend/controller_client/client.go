package controller_client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"backend/models"
)

const defaultSocketPath = "/run/web-controller/controller-daemon.sock"

var configuredSocketPath atomic.Value

func init() { configuredSocketPath.Store(defaultSocketPath) }

// ConfigureSocketPath selects the shared daemon socket before serving requests.
// It rejects paths that could resolve differently from the validated config.
func ConfigureSocketPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, '\x00') {
		return errors.New("controller socket path must be a clean absolute path")
	}
	configuredSocketPath.Store(path)
	return nil
}

func currentSocketPath() string { return configuredSocketPath.Load().(string) }

type FailureStage string

const (
	FailureStageConnect       FailureStage = "connect"
	FailureStageWrite         FailureStage = "write"
	FailureStageRead          FailureStage = "read"
	FailureStageResponseParse FailureStage = "response_parse"
	FailureStageRemoteReject  FailureStage = "remote_reject"
)

var (
	ErrInvalidResponse = errors.New("invalid controller response")
	ErrRemoteRejected  = errors.New("controller rejected command")
)

type RemoteResponse struct {
	OK        bool
	Error     string
	ErrorCode models.IRWireErrorCode
}

type CommandResult struct {
	ResponseBody   string
	FailureStage   FailureStage
	BytesWritten   int
	WriteCompleted bool
	RemoteResponse *RemoteResponse
	Err            error
}

// IsPreSendRejection is true only for a parsed remote rejection carrying one
// of the fixed wire error codes that guarantees IR transmission did not start.
func (result CommandResult) IsPreSendRejection() bool {
	return result.FailureStage == FailureStageRemoteReject &&
		result.RemoteResponse != nil &&
		result.RemoteResponse.ErrorCode.IsPreSendRejection()
}

type dialTimeoutFunc func(network string, address string, timeout time.Duration) (net.Conn, error)

// SendCommand keeps the legacy caller contract while routing all communication
// through the typed result path.
func SendCommand(command string) (string, error) {
	result, err := SendCommandResult(command)
	return result.ResponseBody, err
}

func SendCommandResult(command string) (CommandResult, error) {
	return sendCommandResultAt(command, currentSocketPath(), net.DialTimeout)
}

func sendCommandResult(command string, dial dialTimeoutFunc) (CommandResult, error) {
	return sendCommandResultAt(command, currentSocketPath(), dial)
}

func sendCommandResultAt(command, socketPath string, dial dialTimeoutFunc) (CommandResult, error) {
	result := CommandResult{}
	conn, err := dial("unix", socketPath, 15*time.Second)
	if err != nil {
		return fail(result, FailureStageConnect, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return fail(result, FailureStageWrite, err)
	}

	payload := []byte(command + "\n")
	written, err := conn.Write(payload)
	result.BytesWritten = written
	result.WriteCompleted = written == len(payload)
	if err != nil {
		return fail(result, FailureStageWrite, err)
	}
	if !result.WriteCompleted {
		return fail(result, FailureStageWrite, io.ErrShortWrite)
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	result.ResponseBody = response
	if err != nil {
		return fail(result, FailureStageRead, err)
	}

	var wireResponse struct {
		OK        *bool                  `json:"ok"`
		Error     string                 `json:"error"`
		ErrorCode models.IRWireErrorCode `json:"error_code"`
	}
	if err := json.Unmarshal([]byte(response), &wireResponse); err != nil || wireResponse.OK == nil {
		if err == nil {
			err = errors.New("missing ok field")
		}
		return fail(result, FailureStageResponseParse, fmt.Errorf("%w: %v", ErrInvalidResponse, err))
	}

	result.RemoteResponse = &RemoteResponse{
		OK:        *wireResponse.OK,
		Error:     wireResponse.Error,
		ErrorCode: wireResponse.ErrorCode,
	}
	if !*wireResponse.OK {
		err := ErrRemoteRejected
		if wireResponse.Error != "" {
			err = fmt.Errorf("%w: %s", ErrRemoteRejected, wireResponse.Error)
		}
		return fail(result, FailureStageRemoteReject, err)
	}

	return result, nil
}

func fail(result CommandResult, stage FailureStage, err error) (CommandResult, error) {
	result.FailureStage = stage
	result.Err = err
	return result, err
}
