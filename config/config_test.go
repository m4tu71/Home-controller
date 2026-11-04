package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		HTTPListen: "127.0.0.1:3000", DataRoot: "/var/lib/web-controller/data",
		MaintenanceRoot: "/var/lib/web-controller/maintenance", BackupRoot: "/var/lib/web-controller/backups",
		TrashRoot: "/var/lib/web-controller/trash", TrashRetentionDays: 30,
		SoftwareRoot: "/opt/web-controller/current", WebRoot: "/opt/web-controller/current/web",
		SocketPath:     "/run/web-controller/controller-daemon.sock",
		PicoSerialPath: "/dev/serial/by-id/usb-Raspberry_Pi_Pico_example-if00",
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAndParseArgs(t *testing.T) {
	c := validConfig()
	path := writeConfig(t, `{"http_listen":"127.0.0.1:3000","data_root":"/var/lib/web-controller/data","maintenance_root":"/var/lib/web-controller/maintenance","backup_root":"/var/lib/web-controller/backups","trash_root":"/var/lib/web-controller/trash","trash_retention_days":30,"software_root":"/opt/web-controller/current","web_root":"/opt/web-controller/current/web","socket_path":"/run/web-controller/controller-daemon.sock","pico_serial_path":"/dev/serial/by-id/usb-Raspberry_Pi_Pico_example-if00"}`)
	got, err := Load(path)
	if err != nil || got != c {
		t.Fatalf("Load: %+v %v", got, err)
	}
	gotPath, rest, err := ParseArgs([]string{"--config", path, "maintenance", "restore-dry-run", "backup_1"})
	if err != nil || gotPath != path || len(rest) != 3 {
		t.Fatalf("ParseArgs: %q %v %v", gotPath, rest, err)
	}
	for _, args := range [][]string{{}, {"maintenance"}, {"--config"}, {"--config", "relative.json"}} {
		if _, _, err := ParseArgs(args); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseArgs(%v) = %v", args, err)
		}
	}
}

func TestLoadRejectsFileAndJSONErrors(t *testing.T) {
	for name, body := range map[string]string{
		"type":     `{"trash_retention_days":"30"}`,
		"unknown":  `{"unknown":true}`,
		"trailing": `{}` + "\n" + `{}`,
		"missing":  `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Load = %v", err)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing file = %v", err)
	}
	path := writeConfig(t, `{}`)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("insecure mode = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink = %v", err)
	}
}

func TestValidateRejectsUnsafeValues(t *testing.T) {
	tests := map[string]func(*Config){
		"public HTTP":     func(c *Config) { c.HTTPListen = "0.0.0.0:3000" },
		"invalid port":    func(c *Config) { c.HTTPListen = "127.0.0.1:0" },
		"relative root":   func(c *Config) { c.DataRoot = "data" },
		"overlap":         func(c *Config) { c.BackupRoot = c.DataRoot + "/backups" },
		"bad web root":    func(c *Config) { c.WebRoot = "/var/www/web-controller" },
		"socket overlap":  func(c *Config) { c.SocketPath = c.DataRoot + "/daemon.sock" },
		"retention":       func(c *Config) { c.TrashRetentionDays = 0 },
		"USB placeholder": func(c *Config) { c.PicoSerialPath = "/dev/serial/by-id/CHANGE_ME" },
		"USB path":        func(c *Config) { c.PicoSerialPath = "/dev/ttyACM0" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			mutate(&c)
			if err := c.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate = %v", err)
			}
		})
	}
}

func TestLegacyEnvironmentConflict(t *testing.T) {
	path := writeConfig(t, `{"http_listen":"127.0.0.1:3000","data_root":"/var/lib/web-controller/data","maintenance_root":"/var/lib/web-controller/maintenance","backup_root":"/var/lib/web-controller/backups","trash_root":"/var/lib/web-controller/trash","trash_retention_days":30,"software_root":"/opt/web-controller/current","web_root":"/opt/web-controller/current/web","socket_path":"/run/web-controller/controller-daemon.sock","pico_serial_path":"/dev/serial/by-id/usb-Raspberry_Pi_Pico_example-if00"}`)
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", "/var/lib/web-controller/trash")
	if _, err := Load(path); err != nil {
		t.Fatalf("matching legacy variable: %v", err)
	}
	t.Setenv("WEB_CONTROLLER_TRASH_DIR", "/different")
	if _, err := Load(path); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "WEB_CONTROLLER_TRASH_DIR") {
		t.Fatalf("conflict = %v", err)
	}
}

func TestExampleRequiresPicoSerialEdit(t *testing.T) {
	data, err := os.ReadFile("../examples/config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	err = c.Validate()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "pico_serial_path") {
		t.Fatalf("unedited example = %v", err)
	}
}
