package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
)

const picoBaudRate = 115200

const (
	picoReadTimeout        = 2 * time.Second
	picoCaptureReadTimeout = 7 * time.Second // 5s firmware start wait + frame/serialization margin.
)

var errPicoReadTimeout = errors.New("pico response read timeout")

type picoResponsePort interface {
	SetReadTimeout(time.Duration) error
	Write([]byte) (int, error)
	Read([]byte) (int, error)
	Close() error
}

type timeoutAwarePicoReader struct{ port picoResponsePort }

func (r timeoutAwarePicoReader) Read(data []byte) (int, error) {
	n, err := r.port.Read(data)
	if n == 0 && err == nil {
		return 0, errPicoReadTimeout
	}
	return n, err
}

func sendCommandToPico(command string) (string, error) {
	return "", errors.New("Pico serial path must be configured before sending commands")
}

type picoSerialOpenFunc func(string, *serial.Mode) (picoResponsePort, error)

func openPicoSerial(path string, mode *serial.Mode) (picoResponsePort, error) {
	return serial.Open(path, mode)
}

var configuredPicoSerialOpen picoSerialOpenFunc = openPicoSerial

func sendCommandToPicoAt(path, command string, open picoSerialOpenFunc) (string, error) {
	mode := &serial.Mode{
		BaudRate: picoBaudRate,
	}

	port, err := open(path, mode)
	if err != nil {
		return "", fmt.Errorf("failed to open serial port: %w", err)
	}
	defer port.Close()

	return readPicoResponse(port, command)
}

func readPicoResponse(port picoResponsePort, command string) (string, error) {
	readTimeout := picoReadTimeout
	if command == "IR_CAPTURE" {
		readTimeout = picoCaptureReadTimeout
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		return "", fmt.Errorf("failed to set read timeout: %w", err)
	}

	_, err := port.Write([]byte(command + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to write command: %w", err)
	}

	reader := bufio.NewReader(timeoutAwarePicoReader{port: port})

	response, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return strings.TrimSpace(response), nil
}
