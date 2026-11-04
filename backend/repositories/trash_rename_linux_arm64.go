//go:build linux && arm64

package repositories

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const trashRenameat2Number = 276

func trashRenameNoReplace(source, target string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(target)
	if err != nil {
		return err
	}
	_, _, errno := syscall.RawSyscall6(trashRenameat2Number, uintptr(^uint(99)), uintptr(unsafe.Pointer(from)), uintptr(^uint(99)), uintptr(unsafe.Pointer(to)), 1, 0)
	runtime.KeepAlive(from)
	runtime.KeepAlive(to)
	if errno != 0 {
		return &os.LinkError{Op: "renameat2", Old: source, New: target, Err: errno}
	}
	return nil
}
