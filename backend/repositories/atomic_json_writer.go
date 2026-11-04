package repositories

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// JSONWriter exposes only JSON create/update. File and directory deletion are
// separate operations and must not be implemented through this interface.
type JSONWriter interface {
	WriteJSON(path string, value any, mode os.FileMode) error
}

// AtomicWriteStage identifies where a write stopped. A failure after rename
// needs readback or recovery because the target may already contain new data.
type AtomicWriteStage string

const (
	AtomicWriteMarshal    AtomicWriteStage = "marshal"
	AtomicWriteCreateTemp AtomicWriteStage = "create_temp"
	AtomicWriteWrite      AtomicWriteStage = "write"
	AtomicWriteChmod      AtomicWriteStage = "chmod"
	AtomicWriteFileSync   AtomicWriteStage = "file_sync"
	AtomicWriteFileClose  AtomicWriteStage = "file_close"
	AtomicWriteRename     AtomicWriteStage = "rename"
	AtomicWriteParentSync AtomicWriteStage = "parent_sync"
	AtomicWriteCleanup    AtomicWriteStage = "cleanup"
)

type AtomicWriteError struct {
	Stage          AtomicWriteStage
	TargetReplaced bool
	Err            error
}

func (e *AtomicWriteError) Error() string {
	return fmt.Sprintf("atomic JSON write %s: %v", e.Stage, e.Err)
}

func (e *AtomicWriteError) Unwrap() error {
	return e.Err
}

// AtomicJSONWriter writes one complete JSON file through a temporary file in
// the target's existing parent directory. It does not create directories or
// guard against concurrent logical updates; callers must coordinate those.
type AtomicJSONWriter struct {
	beforeStage func(AtomicWriteStage) error
	writeTemp   func(*os.File, []byte) (int, error)
}

func NewAtomicJSONWriter() *AtomicJSONWriter {
	return &AtomicJSONWriter{}
}

// WriteJSON replaces a target only after the temporary file has been written,
// chmodded, synced, and closed. A successful rename is followed by a parent
// directory sync. TargetReplaced is true only if rename already succeeded.
func (w *AtomicJSONWriter) WriteJSON(target string, value any, mode os.FileMode) (resultErr error) {
	return w.writeJSON(target, value, mode, false)
}

// WriteJSONNew atomically creates a JSON file without replacing an existing
// target. It uses the same staging and durability checks as WriteJSON.
func (w *AtomicJSONWriter) WriteJSONNew(target string, value any, mode os.FileMode) error {
	return w.writeJSON(target, value, mode, true)
}

func (w *AtomicJSONWriter) writeJSON(target string, value any, mode os.FileMode, createOnly bool) (resultErr error) {
	if target == "" {
		return stageError(AtomicWriteCreateTemp, false, errors.New("target path is empty"))
	}
	if mode == 0 || mode&^os.ModePerm != 0 {
		return stageError(AtomicWriteChmod, false, fmt.Errorf("invalid file mode %v", mode))
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return stageError(AtomicWriteMarshal, false, err)
	}

	parent := filepath.Dir(target)
	if err := w.inject(AtomicWriteCreateTemp); err != nil {
		return stageError(AtomicWriteCreateTemp, false, err)
	}
	temp, err := os.CreateTemp(parent, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return stageError(AtomicWriteCreateTemp, false, err)
	}

	tempPath := temp.Name()
	closed := false
	renamed := false
	defer func() {
		if !closed {
			_ = temp.Close()
		}
		if !renamed {
			if cleanupErr := w.cleanupTemp(tempPath); cleanupErr != nil {
				resultErr = errors.Join(resultErr, cleanupErr)
			}
		}
	}()

	if err := w.inject(AtomicWriteWrite); err != nil {
		return stageError(AtomicWriteWrite, false, err)
	}
	n, err := w.write(temp, data)
	if err != nil {
		return stageError(AtomicWriteWrite, false, err)
	}
	if n != len(data) {
		return stageError(AtomicWriteWrite, false, io.ErrShortWrite)
	}

	if err := w.inject(AtomicWriteChmod); err != nil {
		return stageError(AtomicWriteChmod, false, err)
	}
	if err := temp.Chmod(mode); err != nil {
		return stageError(AtomicWriteChmod, false, err)
	}

	if err := w.inject(AtomicWriteFileSync); err != nil {
		return stageError(AtomicWriteFileSync, false, err)
	}
	if err := temp.Sync(); err != nil {
		return stageError(AtomicWriteFileSync, false, err)
	}

	if err := w.inject(AtomicWriteFileClose); err != nil {
		return stageError(AtomicWriteFileClose, false, err)
	}
	if err := temp.Close(); err != nil {
		return stageError(AtomicWriteFileClose, false, err)
	}
	closed = true

	if err := w.inject(AtomicWriteRename); err != nil {
		return stageError(AtomicWriteRename, false, err)
	}
	if createOnly {
		if err := os.Link(tempPath, target); err != nil {
			return stageError(AtomicWriteRename, false, err)
		}
		renamed = true
		if err := os.Remove(tempPath); err != nil {
			return stageError(AtomicWriteCleanup, true, err)
		}
	} else {
		if err := os.Rename(tempPath, target); err != nil {
			return stageError(AtomicWriteRename, false, err)
		}
		renamed = true
	}

	parentDir, err := os.Open(parent)
	if err != nil {
		return stageError(AtomicWriteParentSync, true, err)
	}
	parentClosed := false
	defer func() {
		if !parentClosed {
			_ = parentDir.Close()
		}
	}()
	if err := w.inject(AtomicWriteParentSync); err != nil {
		return stageError(AtomicWriteParentSync, true, err)
	}
	if err := parentDir.Sync(); err != nil {
		return stageError(AtomicWriteParentSync, true, err)
	}
	if err := parentDir.Close(); err != nil {
		return stageError(AtomicWriteParentSync, true, err)
	}
	parentClosed = true
	return nil
}

func (w *AtomicJSONWriter) write(temp *os.File, data []byte) (int, error) {
	if w != nil && w.writeTemp != nil {
		return w.writeTemp(temp, data)
	}
	return temp.Write(data)
}

func (w *AtomicJSONWriter) inject(stage AtomicWriteStage) error {
	if w != nil && w.beforeStage != nil {
		return w.beforeStage(stage)
	}
	return nil
}

func (w *AtomicJSONWriter) cleanupTemp(path string) error {
	if err := w.inject(AtomicWriteCleanup); err != nil {
		return stageError(AtomicWriteCleanup, false, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return stageError(AtomicWriteCleanup, false, err)
	}
	return nil
}

func stageError(stage AtomicWriteStage, replaced bool, err error) *AtomicWriteError {
	return &AtomicWriteError{Stage: stage, TargetReplaced: replaced, Err: err}
}
