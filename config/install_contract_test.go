package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func installationExample(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../examples/config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInstallExampleLoadsAfterSerialEdit(t *testing.T) {
	c := installationExample(t)
	c.PicoSerialPath = "/dev/serial/by-id/INSTALLATION_TEST_NOT_CONNECTED"
	for i, key := range legacyEnvironment {
		t.Setenv(key, []string{c.MaintenanceRoot, c.BackupRoot, c.TrashRoot, strconv.Itoa(c.TrashRetentionDays)}[i])
	}
	raw, err := os.ReadFile("../examples/config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("CHANGE_ME"), []byte("INSTALLATION_TEST_NOT_CONNECTED"))
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != c {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("world-readable config accepted")
	}
}

func templateDirectives(t *testing.T, name string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../examples/systemd/" + name)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			result[key] = value
		}
	}
	return result
}

func TestInstallUnitsMatchArgumentAndSocketContract(t *testing.T) {
	c := installationExample(t)
	for _, binary := range []string{"backend", "controller-daemon"} {
		name := "web-controller-backend.service"
		if binary == "controller-daemon" {
			name = "web-controller-daemon.service"
		}
		d := templateDirectives(t, name)
		if d["User"] != "web-controller" || d["Group"] != "web-controller" {
			t.Fatal("unit identity mismatch")
		}
		command := strings.Fields(d["ExecStart"])
		if len(command) != 3 || command[0] != filepath.Join(c.SoftwareRoot, "bin", binary) {
			t.Fatalf("ExecStart = %v", command)
		}
		path, rest, err := ParseArgs(command[1:])
		if err != nil || path != "/etc/web-controller/config.json" || len(rest) != 0 {
			t.Fatalf("arguments = %q %v %v", path, rest, err)
		}
		if d["WorkingDirectory"] != c.SoftwareRoot {
			t.Fatal("working directory mismatch")
		}
		for _, key := range legacyEnvironment {
			if !strings.Contains(d["UnsetEnvironment"], key) {
				t.Fatalf("legacy environment not cleared: %s", key)
			}
		}
		if binary == "controller-daemon" {
			if filepath.Join("/run", d["RuntimeDirectory"]) != filepath.Dir(c.SocketPath) || d["RuntimeDirectoryMode"] != "0750" || d["SupplementaryGroups"] != "dialout" {
				t.Fatal("socket ownership/mode or serial access contract mismatch")
			}
		} else if d["Wants"] != "web-controller-daemon.service" || d["Requires"] != "" {
			t.Fatal("Backend must remain available when daemon is absent")
		}
	}
}

func TestInstallNginxMatchesHTTPAndWeb(t *testing.T) {
	c := installationExample(t)
	raw, err := os.ReadFile("../examples/nginx/web-controller.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"listen 80;", "listen [::]:80;", "root " + c.WebRoot + ";", "proxy_pass http://" + c.HTTPListen + ";", "location /api/", "try_files $uri $uri/ /index.html;"} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("missing directive: %s", fragment)
		}
	}
	if strings.Contains(string(raw), "proxy_pass http://"+c.HTTPListen+"/;") {
		t.Fatal("proxy must preserve /api URI")
	}
}
