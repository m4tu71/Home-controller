package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.bug.st/serial"
	appconfig "webcontroller/config"
)

func TestRunUsesConfiguredSocketAndPicoPaths(t *testing.T) {
	base := t.TempDir()
	config := appconfig.Config{
		HTTPListen: "127.0.0.1:3000", DataRoot: filepath.Join(base, "data"),
		MaintenanceRoot: filepath.Join(base, "maintenance"), BackupRoot: filepath.Join(base, "backups"),
		TrashRoot: filepath.Join(base, "trash"), TrashRetentionDays: 30,
		SoftwareRoot: filepath.Join(base, "software"), WebRoot: filepath.Join(base, "software", "web"),
		SocketPath: filepath.Join(base, "run", "daemon.sock"), PicoSerialPath: "/dev/serial/by-id/configured-pico",
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"WEB_CONTROLLER_MAINTENANCE_STATE_DIR": config.MaintenanceRoot,
		"WEB_CONTROLLER_BACKUP_DIR":            config.BackupRoot,
		"WEB_CONTROLLER_TRASH_DIR":             config.TrashRoot,
		"WEB_CONTROLLER_TRASH_RETENTION_DAYS":  "30",
	} {
		t.Setenv(name, value)
	}
	previousForwarder := forwardCommandToPico
	previousOpener := configuredPicoSerialOpen
	t.Cleanup(func() {
		forwardCommandToPico = previousForwarder
		configuredPicoSerialOpen = previousOpener
	})
	port := &fakePicoResponsePort{response: []byte("{\"ok\":true}\n")}
	var openedPath string
	configuredPicoSerialOpen = func(path string, mode *serial.Mode) (picoResponsePort, error) {
		openedPath = path
		if mode.BaudRate != picoBaudRate {
			t.Fatalf("baud rate=%d, want %d", mode.BaudRate, picoBaudRate)
		}
		return port, nil
	}
	startErr := errors.New("stop after config wiring")
	startCalled := false
	err = runWithSocketServer([]string{"--config", configPath}, func(socketPath string) error {
		startCalled = true
		if socketPath != config.SocketPath {
			t.Fatalf("socket path=%q, want %q", socketPath, config.SocketPath)
		}
		if got := handleCommand("IR_TEST"); got != `{"ok":true}` {
			t.Fatalf("configured serial response=%s", got)
		}
		if openedPath != config.PicoSerialPath {
			t.Fatalf("serial path=%q, want %q", openedPath, config.PicoSerialPath)
		}
		return startErr
	})
	if !startCalled || !errors.Is(err, startErr) {
		t.Fatalf("startCalled=%t err=%v", startCalled, err)
	}
	if port.readTimeout != 2*time.Second || port.command != "IR_TEST\n" {
		t.Fatalf("serial settings timeout=%v command=%q", port.readTimeout, port.command)
	}
}

func TestRunRejectsMissingConfigBeforeSocketStartup(t *testing.T) {
	called := false
	err := runWithSocketServer([]string{"--config", filepath.Join(t.TempDir(), "missing.json")}, func(string) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("run error=%v start called=%t", err, called)
	}
}
