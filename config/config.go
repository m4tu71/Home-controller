package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var ErrInvalid = errors.New("invalid web-controller config")

// Config is shared by the backend and controller-daemon. Paths identify
// installation boundaries; creating or changing those paths is a later step.
type Config struct {
	HTTPListen         string `json:"http_listen"`
	DataRoot           string `json:"data_root"`
	MaintenanceRoot    string `json:"maintenance_root"`
	BackupRoot         string `json:"backup_root"`
	TrashRoot          string `json:"trash_root"`
	TrashRetentionDays int    `json:"trash_retention_days"`
	SoftwareRoot       string `json:"software_root"`
	WebRoot            string `json:"web_root"`
	SocketPath         string `json:"socket_path"`
	PicoSerialPath     string `json:"pico_serial_path"`
}

const maxConfigBytes = 64 << 10

var legacyEnvironment = []string{
	"WEB_CONTROLLER_MAINTENANCE_STATE_DIR",
	"WEB_CONTROLLER_BACKUP_DIR",
	"WEB_CONTROLLER_TRASH_DIR",
	"WEB_CONTROLLER_TRASH_RETENTION_DAYS",
}

// ParseArgs accepts --config before the optional backend maintenance command.
func ParseArgs(args []string) (string, []string, error) {
	if len(args) < 2 || args[0] != "--config" || strings.TrimSpace(args[1]) == "" {
		return "", nil, fmt.Errorf("%w: usage: --config <absolute-path> [maintenance ...]", ErrInvalid)
	}
	if !filepath.IsAbs(args[1]) || filepath.Clean(args[1]) != args[1] {
		return "", nil, fmt.Errorf("%w: --config must be a clean absolute path", ErrInvalid)
	}
	return args[1], args[2:], nil
}

// Load permits legacy variables only when they exactly match the file.
// This avoids a silent choice between two different installations.
func Load(path string) (Config, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Config{}, fmt.Errorf("%w: config path must be clean and absolute", ErrInvalid)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Config{}, fmt.Errorf("%w: config file %s: %v", ErrInvalid, path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) || (info.Mode().Perm() != 0600 && info.Mode().Perm() != 0640) {
		return Config{}, fmt.Errorf("%w: config file must be regular, owned by current user or root, and mode 0600 or 0640", ErrInvalid)
	}
	if info.Size() > maxConfigBytes {
		return Config{}, fmt.Errorf("%w: config file exceeds %d bytes", ErrInvalid, maxConfigBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("%w: open config: %v", ErrInvalid, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Config{}, fmt.Errorf("%w: config file changed during open", ErrInvalid)
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxConfigBytes+1))
	decoder.DisallowUnknownFields()
	var c Config
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, fmt.Errorf("%w: trailing JSON or config exceeds %d bytes", ErrInvalid, maxConfigBytes)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	want := []string{c.MaintenanceRoot, c.BackupRoot, c.TrashRoot, strconv.Itoa(c.TrashRetentionDays)}
	for i, key := range legacyEnvironment {
		if value, set := os.LookupEnv(key); set && value != want[i] {
			return Config{}, fmt.Errorf("%w: legacy environment variable %s conflicts with config file", ErrInvalid, key)
		}
	}
	return c, nil
}

func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.HTTPListen)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return fmt.Errorf("%w: http_listen must use a loopback address and port", ErrInvalid)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%w: http_listen port must be 1..65535", ErrInvalid)
	}
	paths := map[string]string{
		"data_root": c.DataRoot, "maintenance_root": c.MaintenanceRoot,
		"backup_root": c.BackupRoot, "trash_root": c.TrashRoot,
		"software_root": c.SoftwareRoot, "web_root": c.WebRoot,
		"socket_path": c.SocketPath, "pico_serial_path": c.PicoSerialPath,
	}
	for name, value := range paths {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value || value == "/" || strings.Contains(value, "CHANGE_ME") {
			return fmt.Errorf("%w: %s must be a clean absolute path without placeholders", ErrInvalid, name)
		}
	}
	if !strings.HasPrefix(c.PicoSerialPath, "/dev/serial/by-id/") || c.PicoSerialPath == "/dev/serial/by-id/" {
		return fmt.Errorf("%w: pico_serial_path must identify a USB device under /dev/serial/by-id", ErrInvalid)
	}
	if c.SocketPath == c.PicoSerialPath {
		return fmt.Errorf("%w: socket_path and pico_serial_path must differ", ErrInvalid)
	}
	if c.TrashRetentionDays < 1 {
		return fmt.Errorf("%w: trash_retention_days must be positive", ErrInvalid)
	}
	if !inside(c.SoftwareRoot, c.WebRoot) || c.SoftwareRoot == c.WebRoot {
		return fmt.Errorf("%w: web_root must be inside software_root", ErrInvalid)
	}
	roots := []string{c.DataRoot, c.MaintenanceRoot, c.BackupRoot, c.TrashRoot, c.SoftwareRoot}
	for i, a := range roots {
		for _, b := range roots[i+1:] {
			if inside(a, b) || inside(b, a) {
				return fmt.Errorf("%w: data, maintenance, backup, trash and software roots must not overlap", ErrInvalid)
			}
		}
	}
	for _, root := range roots {
		if inside(root, c.SocketPath) || inside(c.SocketPath, root) || inside(root, c.PicoSerialPath) || inside(c.PicoSerialPath, root) {
			return fmt.Errorf("%w: socket and serial paths must be outside protected roots", ErrInvalid)
		}
	}
	return nil
}

func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../")))
}
