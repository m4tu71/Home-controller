package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"backend/models"
)

const (
	MaintenanceUnchanged          = "unchanged"
	MaintenanceAppliedUnconfirmed = "applied_unconfirmed"
	MaintenanceTrashedUnconfirmed = "trashed_unconfirmed"
	MaintenanceAmbiguous          = "ambiguous"
)

type MaintenanceFilesystemInspector struct {
	RuntimeRoot string
	TrashRoot   string
}

// Inspect compares the current filesystem against persisted snapshots only.
// A missing/invalid snapshot or unsafe path is ambiguous, never a match.
func (i MaintenanceFilesystemInspector) Inspect(action models.MaintenanceOperationAction) models.MaintenanceRecoveryAction {
	result := models.MaintenanceRecoveryAction{ActionID: action.ID, Classification: MaintenanceAmbiguous}
	var before, intended models.MaintenanceSnapshot
	if len(action.BeforeState) == 0 || len(action.IntendedState) == 0 || json.Unmarshal(action.BeforeState, &before) != nil || json.Unmarshal(action.IntendedState, &intended) != nil {
		result.Reason = "missing_or_invalid_snapshot"
		return result
	}
	if err := validateSnapshotPair(before, intended); err != nil {
		result.Reason = "invalid_snapshot"
		return result
	}
	actual := models.MaintenanceSnapshot{}
	if before.Runtime != nil {
		entry, err := i.observe(i.RuntimeRoot, before.Runtime.Path)
		if err != nil {
			result.Reason = "runtime_read_failed"
			return result
		}
		actual.Runtime = &entry
	}
	if before.Trash != nil {
		entry, err := i.observe(i.TrashRoot, before.Trash.Path)
		if err != nil {
			result.Reason = "trash_read_failed"
			return result
		}
		actual.Trash = &entry
	}
	if sameSnapshot(actual, before) {
		result.Classification = MaintenanceUnchanged
		return result
	}
	if sameSnapshot(actual, intended) {
		if intended.Trash != nil && intended.Trash.Exists && (before.Trash == nil || !before.Trash.Exists) {
			result.Classification = MaintenanceTrashedUnconfirmed
		} else {
			result.Classification = MaintenanceAppliedUnconfirmed
		}
		return result
	}
	result.Reason = "filesystem_mismatch"
	return result
}

func validateSnapshotPair(before, intended models.MaintenanceSnapshot) error {
	if before.Runtime == nil && before.Trash == nil {
		return errors.New("snapshot has no paths")
	}
	for _, pair := range [][2]*models.MaintenanceSnapshotEntry{{before.Runtime, intended.Runtime}, {before.Trash, intended.Trash}} {
		if (pair[0] == nil) != (pair[1] == nil) {
			return errors.New("snapshot paths differ")
		}
		if pair[0] == nil {
			continue
		}
		if pair[0].Path != pair[1].Path || !safeRelativePath(pair[0].Path) {
			return errors.New("invalid or changed snapshot path")
		}
		for _, e := range pair {
			if e.Exists && ((e.Kind != "file" && e.Kind != "directory") || len(e.SHA256) != 64) {
				return errors.New("incomplete expected object")
			}
			if !e.Exists && (e.Kind != "" || e.SHA256 != "") {
				return errors.New("invalid absent object")
			}
		}
	}
	return nil
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func sameSnapshot(a, b models.MaintenanceSnapshot) bool {
	for _, pair := range [][2]*models.MaintenanceSnapshotEntry{{a.Runtime, b.Runtime}, {a.Trash, b.Trash}} {
		if (pair[0] == nil) != (pair[1] == nil) {
			return false
		}
		if pair[0] == nil {
			continue
		}
		if *pair[0] != *pair[1] {
			return false
		}
	}
	return true
}

func (i MaintenanceFilesystemInspector) observe(root, rel string) (models.MaintenanceSnapshotEntry, error) {
	e := models.MaintenanceSnapshotEntry{Path: rel}
	if root == "" || !filepath.IsAbs(root) || !safeRelativePath(rel) {
		return e, errors.New("unconfigured root or unsafe relative path")
	}
	root = filepath.Clean(root)
	if err := rejectObservedSymlinks(root, rel); err != nil {
		return e, err
	}
	path := filepath.Join(root, rel)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	if info.Mode().IsRegular() {
		digest, err := hashObservedFile(path, info)
		if err != nil {
			return e, err
		}
		e.Exists, e.Kind, e.SHA256 = true, "file", digest
		return e, nil
	}
	if info.IsDir() {
		digest, err := hashObservedDirectory(path)
		if err != nil {
			return e, err
		}
		e.Exists, e.Kind, e.SHA256 = true, "directory", digest
		return e, nil
	}
	return e, errors.New("unsupported filesystem object")
}

func rejectObservedSymlinks(root, rel string) error {
	for p := root; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in root")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	p := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in observed path")
		}
	}
	return nil
}

func hashObservedFile(path string, info os.FileInfo) (string, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("file changed during open")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", errors.New("file changed during read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashObservedDirectory(path string) (string, error) {
	h := sha256.New()
	if err := hashDirectoryEntries(h, path, ""); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashDirectoryEntries(h hash.Hash, path, prefix string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(path, name)
		info, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in observed directory")
		}
		rel := filepath.ToSlash(filepath.Join(prefix, name))
		if info.IsDir() {
			if _, err := fmt.Fprintf(h, "d:%s\n", rel); err != nil {
				return err
			}
			if err := hashDirectoryEntries(h, child, rel); err != nil {
				return err
			}
		} else if info.Mode().IsRegular() {
			digest, err := hashObservedFile(child, info)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(h, "f:%s:%s\n", rel, digest); err != nil {
				return err
			}
		} else {
			return errors.New("unsupported object in observed directory")
		}
	}
	return nil
}
