package main

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func socketTestPath(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "socket-dir")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(directory, os.Geteuid(), os.Getegid()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, socketDirMode); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "controller.sock")
}

func TestListenControllerSocketModeAndProtocol(t *testing.T) {
	path := socketTestPath(t)
	listener, cleanup, err := listenControllerSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != socketFileMode {
		t.Fatalf("socket mode=%v, want socket 0660", info.Mode())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) {
		t.Fatalf("socket owner/group does not match daemon/shared group: %+v", stat)
	}
	previousForwarder := forwardCommandToPico
	forwardCommandToPico = func(command string) (string, error) {
		if command != "LED_STATUS" {
			t.Errorf("command=%q, want LED_STATUS", command)
		}
		return `{"ok":true}`, nil
	}
	t.Cleanup(func() { forwardCommandToPico = previousForwarder })
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			handleConnection(connection)
		}
	}()
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("LED_STATUS\n")); err != nil {
		t.Fatal(err)
	}
	response, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil || strings.TrimSpace(response) != `{"ok":true}` {
		t.Fatalf("response=%q err=%v", response, err)
	}
}

func TestListenControllerSocketRemovesOnlyStaleOwnedSocket(t *testing.T) {
	path := socketTestPath(t)
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	listener, cleanup, err := listenControllerSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned socket remains after cleanup: %v", err)
	}
	_ = listener
}

func TestListenControllerSocketRejectsLiveOrNonSocketPaths(t *testing.T) {
	path := socketTestPath(t)
	live, cleanup, err := listenControllerSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := removeStaleControllerSocket(path, uint32(os.Geteuid())); err == nil || !strings.Contains(err.Error(), "already accepting") {
		t.Fatalf("live socket cleanup error=%v", err)
	}
	if _, _, err := listenControllerSocket(path); err == nil || !strings.Contains(err.Error(), "already locked") {
		t.Fatalf("duplicate daemon startup error=%v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("live socket was removed: %v", err)
	}
	_ = live

	separateSocketPath := socketTestPath(t)
	regularPath := filepath.Join(filepath.Dir(separateSocketPath), "not-a-socket")
	if err := os.WriteFile(regularPath, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listenControllerSocket(regularPath); err == nil || !strings.Contains(err.Error(), "expected daemon-owned Unix socket") {
		t.Fatalf("regular path error=%v", err)
	}
	contents, err := os.ReadFile(regularPath)
	if err != nil || string(contents) != "keep" {
		t.Fatalf("regular path changed: %q %v", contents, err)
	}

	symlinkPath := filepath.Join(filepath.Dir(separateSocketPath), "symlink")
	if err := os.Symlink(regularPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listenControllerSocket(symlinkPath); err == nil {
		t.Fatal("symlink socket path unexpectedly accepted")
	}
}

func TestListenControllerSocketCleanupPreservesReplacement(t *testing.T) {
	path := socketTestPath(t)
	first, cleanupFirst, err := listenControllerSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	cleanupFirst()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket removed by prior daemon cleanup: %v", err)
	}
	_ = first
}

func TestSocketServerRejectsDirectoryLockHeldByAnotherStart(t *testing.T) {
	path := socketTestPath(t)
	_, cleanup, err := listenControllerSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, _, err := listenControllerSocket(path); err == nil || !strings.Contains(err.Error(), "already locked") {
		t.Fatalf("second startup error=%v", err)
	}
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("socket state after rejected second start: %v", err)
	}
	_ = connection.Close()
}

func TestListenControllerSocketRejectsUnsafeDirectoryModeAndPath(t *testing.T) {
	path := socketTestPath(t)
	if err := os.Chmod(filepath.Dir(path), 0770); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listenControllerSocket(path); err == nil || !strings.Contains(err.Error(), "mode 0750") {
		t.Fatalf("unsafe directory mode error=%v", err)
	}
	if _, _, err := listenControllerSocket("relative/controller.sock"); err == nil || !strings.Contains(err.Error(), "clean absolute path") {
		t.Fatalf("relative path error=%v", err)
	}
}
