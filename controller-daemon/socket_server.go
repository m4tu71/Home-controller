package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	socketFileMode = 0660
	socketDirMode  = 0750
)

func startSocketServer(path string) error {
	listener, cleanup, err := listenControllerSocket(path)
	if err != nil {
		return err
	}
	defer cleanup()

	log.Println("controller-daemon started:", path)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("accept error:", err)
			continue
		}

		go handleConnection(conn)
	}
}

// listenControllerSocket binds only inside a daemon-owned directory whose
// primary group is shared with Backend. The directory lock serializes stale
// socket recovery between daemon starts, and cleanup removes only the socket
// inode created here.
func listenControllerSocket(path string) (net.Listener, func(), error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, nil, errors.New("controller socket path must be a clean absolute path")
	}
	parent := filepath.Dir(path)
	if err := rejectSocketSymlinkAncestors(parent); err != nil {
		return nil, nil, err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return nil, nil, fmt.Errorf("socket directory unavailable: %w", err)
	}
	stat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !parentInfo.IsDir() || !ok || stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) || parentInfo.Mode().Perm() != socketDirMode {
		uid := uint32(0)
		if ok {
			uid = stat.Uid
		}
		return nil, nil, fmt.Errorf("socket directory must be daemon-owned, primary-group-owned, and mode 0750 (uid=%d mode=%#o)", uid, parentInfo.Mode().Perm())
	}
	directory, err := os.Open(parent)
	if err != nil {
		return nil, nil, fmt.Errorf("open socket directory: %w", err)
	}
	openedDirectory, err := directory.Stat()
	if err != nil || !os.SameFile(parentInfo, openedDirectory) {
		_ = directory.Close()
		return nil, nil, errors.New("socket directory changed while opening")
	}
	if err := syscall.Flock(int(directory.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		directory.Close()
		return nil, nil, fmt.Errorf("socket directory is already locked: %w", err)
	}
	unlock := func() {
		_ = syscall.Flock(int(directory.Fd()), syscall.LOCK_UN)
		_ = directory.Close()
	}
	if err := removeStaleControllerSocket(path, uint32(os.Geteuid())); err != nil {
		unlock()
		return nil, nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		unlock()
		return nil, nil, fmt.Errorf("listen on controller socket: %w", err)
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		unlock()
		return nil, nil, errors.New("unexpected controller socket listener type")
	}
	unixListener.SetUnlinkOnClose(false)
	createdInfo, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		unlock()
		return nil, nil, fmt.Errorf("inspect newly created controller socket: %w", err)
	}
	createdStat, ok := createdInfo.Sys().(*syscall.Stat_t)
	if createdInfo.Mode()&os.ModeSocket == 0 || !ok || createdStat.Uid != uint32(os.Geteuid()) || createdStat.Gid != stat.Gid {
		_ = listener.Close()
		unlock()
		return nil, nil, errors.New("new controller socket has unexpected type or owner")
	}
	if err := os.Chmod(path, socketFileMode); err != nil {
		_ = listener.Close()
		_ = removeControllerSocketIfSame(path, createdInfo, uint32(os.Geteuid()))
		unlock()
		return nil, nil, fmt.Errorf("set controller socket mode: %w", err)
	}
	chmodInfo, err := os.Lstat(path)
	if err != nil || !os.SameFile(createdInfo, chmodInfo) {
		_ = listener.Close()
		_ = removeControllerSocketIfSame(path, createdInfo, uint32(os.Geteuid()))
		unlock()
		return nil, nil, errors.New("controller socket path changed while setting mode")
	}
	chmodStat, ok := chmodInfo.Sys().(*syscall.Stat_t)
	if chmodInfo.Mode().Perm() != socketFileMode || !ok || chmodStat.Gid != stat.Gid {
		_ = listener.Close()
		_ = removeControllerSocketIfSame(path, createdInfo, uint32(os.Geteuid()))
		unlock()
		return nil, nil, errors.New("controller socket mode or group does not match shared socket policy")
	}
	cleanup := func() {
		_ = listener.Close()
		if err := removeControllerSocketIfSame(path, createdInfo, uint32(os.Geteuid())); err != nil {
			log.Printf("controller socket cleanup refused: %v", err)
		}
		unlock()
	}
	return listener, cleanup, nil
}

func rejectSocketSymlinkAncestors(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect socket path ancestor %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("socket path contains symlink ancestor %s", current)
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func removeStaleControllerSocket(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing controller socket: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSocket == 0 || !ok || stat.Uid != uid {
		return errors.New("refusing to remove existing controller path: expected daemon-owned Unix socket")
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return errors.New("controller socket is already accepting connections")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("cannot establish that existing controller socket is stale: %w", dialErr)
	}
	return removeControllerSocketIfSame(path, info, uid)
}

func removeControllerSocketIfSame(path string, expected os.FileInfo, uid uint32) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := current.Sys().(*syscall.Stat_t)
	if !os.SameFile(expected, current) || current.Mode()&os.ModeSocket == 0 || !ok || stat.Uid != uid {
		return errors.New("refusing to remove replaced controller socket path")
	}
	return os.Remove(path)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	command, err := reader.ReadString('\n')
	if err != nil {
		log.Println("read error:", err)
		return
	}

	command = strings.TrimSpace(command)

	log.Printf("command received command=%s", command)

	response := handleCommand(command)

	_, err = conn.Write([]byte(response + "\n"))
	if err != nil {
		log.Println("write error:", err)
		return
	}

	log.Printf("response sent response=%s", response)
}
