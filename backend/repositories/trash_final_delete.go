package repositories

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"backend/models"
)

var (
	ErrTrashFinalDeleteState     = errors.New("Trash entry cannot be finally deleted")
	ErrTrashFinalDeleteRetention = errors.New("Trash retention has not elapsed")
	ErrTrashClockRegression      = errors.New("clock moved backwards since Trash was checked")
)

type TrashFinalDeleteFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Paths are relative to the private entry directory and always start at payload.
type TrashFinalDeleteSnapshot struct {
	Entry       models.TrashEntry      `json:"entry"`
	Files       []TrashFinalDeleteFile `json:"files"`
	Directories []string               `json:"directories"`
	CurrentTime time.Time              `json:"current_time"`
}

type TrashFinalDeleteResult struct {
	EntryID      string `json:"entry_id"`
	PlanID       string `json:"plan_id"`
	OperationID  string `json:"operation_id"`
	DeletedFiles int64  `json:"deleted_files"`
	TotalFiles   int64  `json:"total_files"`
	Status       string `json:"status"`
}

// PreviewFinalDelete records the latest trusted clock reading even when the
// entry is not yet eligible. It never changes the payload.
func (r *TrashRepository) PreviewFinalDelete(id string) (TrashFinalDeleteSnapshot, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return TrashFinalDeleteSnapshot{}, ErrMaintenanceID
	}
	var snapshot TrashFinalDeleteSnapshot
	err := r.withLock(func() error {
		entry, err := r.loadLocked(id)
		if err != nil {
			return err
		}
		if entry.Status != "sealed" || entry.DeletedAt == nil || entry.EligibleAfter == nil {
			return ErrTrashFinalDeleteState
		}
		now := r.now().UTC()
		if now.Before(*entry.DeletedAt) || (entry.LastCheckedAt != nil && now.Before(*entry.LastCheckedAt)) {
			return ErrTrashClockRegression
		}
		payload := filepath.Join(r.config.Dir, id, "payload")
		inventory, err := trashInventory(payload)
		if err != nil || inventory != entry.BeforeInventory {
			return errors.Join(ErrTrashChanged, err)
		}
		files, dirs, err := inspectFinalDeletePayload(filepath.Join(r.config.Dir, id), false)
		if err != nil {
			return err
		}
		entry.LastCheckedAt = &now
		if err := r.writer.WriteJSON(filepath.Join(r.config.Dir, id, "metadata.json"), entry, 0600); err != nil {
			return err
		}
		snapshot = TrashFinalDeleteSnapshot{Entry: entry, Files: files, Directories: dirs, CurrentTime: now}
		return nil
	})
	return snapshot, err
}

// DeleteFinal removes only paths captured in a saved plan. Missing planned
// paths are accepted solely when resuming this same plan and operation.
func (r *TrashRepository) DeleteFinal(expected TrashFinalDeleteSnapshot, planID, operationID string, progress func(int64) error) (TrashFinalDeleteResult, error) {
	result := TrashFinalDeleteResult{EntryID: expected.Entry.ID, PlanID: planID, OperationID: operationID, TotalFiles: int64(len(expected.Files))}
	if !maintenanceIDPattern.MatchString(planID) || !maintenanceIDPattern.MatchString(operationID) || !validFinalDeleteSnapshot(expected) || progress == nil {
		return result, ErrTrashBoundary
	}
	err := r.withLock(func() error {
		entry, err := r.loadLocked(expected.Entry.ID)
		if err != nil {
			return err
		}
		now := r.now().UTC()
		if now.Before(*expected.Entry.DeletedAt) || now.Before(*expected.Entry.EligibleAfter) {
			return ErrTrashFinalDeleteRetention
		}
		if entry.LastCheckedAt == nil || now.Before(*entry.LastCheckedAt) {
			return ErrTrashClockRegression
		}
		if !sameFinalDeleteIdentity(entry, expected.Entry) {
			return ErrTrashChanged
		}
		resuming := entry.Status == "final_deleting" && entry.FinalDeletePlanID == planID && entry.FinalDeleteOperationID == operationID
		if !resuming && (entry.Status != "sealed" || !reflect.DeepEqual(entry, expected.Entry)) {
			return ErrTrashFinalDeleteState
		}
		entryDir := filepath.Join(r.config.Dir, entry.ID)
		files, dirs, err := inspectFinalDeletePayload(entryDir, resuming)
		if err != nil {
			return err
		}
		if !finalDeleteSubset(files, dirs, expected.Files, expected.Directories, resuming) {
			return ErrTrashChanged
		}
		if !resuming {
			inventory, err := trashInventory(filepath.Join(entryDir, "payload"))
			if err != nil || inventory != entry.BeforeInventory {
				return errors.Join(ErrTrashChanged, err)
			}
			entry.Status = "final_deleting"
			entry.RestoreAvailable = false
			entry.FinalDeletePlanID = planID
			entry.FinalDeleteOperationID = operationID
			entry.FinalDeleteFilesTotal = int64(len(expected.Files))
		}
		entry.FinalDeleteFilesDeleted = int64(len(expected.Files) - len(files))
		entry.LastCheckedAt = &now
		metadata := filepath.Join(entryDir, "metadata.json")
		if err := r.writer.WriteJSON(metadata, entry, 0600); err != nil {
			return err
		}
		result.DeletedFiles = entry.FinalDeleteFilesDeleted
		remaining := make(map[string]TrashFinalDeleteFile, len(files))
		for _, file := range files {
			remaining[file.Path] = file
		}
		for _, planned := range expected.Files {
			if _, exists := remaining[planned.Path]; !exists {
				continue
			}
			path := filepath.Join(entryDir, filepath.FromSlash(planned.Path))
			if err := rejectSymlinkAncestors(path); err != nil {
				return err
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() != planned.Size {
				return errors.Join(ErrTrashChanged, err)
			}
			hash, err := trashFileHash(path, info)
			if err != nil || hash != planned.SHA256 {
				return errors.Join(ErrTrashChanged, err)
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := r.syncDir(filepath.Dir(path)); err != nil {
				return err
			}
			entry.FinalDeleteFilesDeleted++
			result.DeletedFiles = entry.FinalDeleteFilesDeleted
			if err := r.writer.WriteJSON(metadata, entry, 0600); err != nil {
				return err
			}
			if err := progress(result.DeletedFiles); err != nil {
				return err
			}
		}
		// Remove empty directories from deepest to shallowest. os.Remove refuses
		// any unexpected remaining child; no recursive deletion is used.
		allDirs := append([]string(nil), expected.Directories...)
		sort.Slice(allDirs, func(i, j int) bool {
			if strings.Count(allDirs[i], "/") == strings.Count(allDirs[j], "/") {
				return allDirs[i] > allDirs[j]
			}
			return strings.Count(allDirs[i], "/") > strings.Count(allDirs[j], "/")
		})
		for _, relative := range allDirs {
			path := filepath.Join(entryDir, filepath.FromSlash(relative))
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			if err := rejectSymlinkAncestors(path); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := r.syncDir(filepath.Dir(path)); err != nil {
				return err
			}
		}
		if _, err := os.Lstat(filepath.Join(entryDir, "payload")); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrTrashChanged, err)
		}
		entry.Status = "final_deleted"
		entry.FinalDeletedAt = &now
		if err := r.writer.WriteJSON(metadata, entry, 0600); err != nil {
			return err
		}
		result.Status = entry.Status
		return nil
	})
	if err != nil {
		result.Status = "final_deleting"
	}
	return result, err
}

func inspectFinalDeletePayload(entryDir string, allowMissing bool) ([]TrashFinalDeleteFile, []string, error) {
	root := filepath.Join(entryDir, "payload")
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) && allowMissing {
		return []TrashFinalDeleteFile{}, []string{}, nil
	} else if err != nil {
		return nil, nil, err
	}
	files := []TrashFinalDeleteFile{}
	dirs := []string{}
	var walk func(string, string) error
	walk = func(path, rel string) error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			hash, err := trashFileHash(path, info)
			if err != nil {
				return err
			}
			files = append(files, TrashFinalDeleteFile{Path: rel, Size: info.Size(), SHA256: hash})
			return nil
		}
		if !info.IsDir() {
			return ErrTrashChanged
		}
		dirs = append(dirs, rel)
		children, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := walk(filepath.Join(path, child.Name()), filepath.ToSlash(filepath.Join(rel, child.Name()))); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, "payload"); err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Strings(dirs)
	return files, dirs, nil
}

func validFinalDeletePath(path string) bool {
	return validTrashRelative(path) && (path == "payload" || strings.HasPrefix(path, "payload/"))
}

func validFinalDeleteSnapshot(snapshot TrashFinalDeleteSnapshot) bool {
	if !validTrashEntry(snapshot.Entry, snapshot.Entry.ID) || snapshot.Entry.Status != "sealed" || snapshot.Entry.LastCheckedAt == nil || !snapshot.CurrentTime.Equal(*snapshot.Entry.LastCheckedAt) {
		return false
	}
	seen := map[string]bool{}
	for _, file := range snapshot.Files {
		if !validFinalDeletePath(file.Path) || file.Size < 0 || len(file.SHA256) != 64 || seen[file.Path] {
			return false
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return false
		}
		seen[file.Path] = true
	}
	for _, dir := range snapshot.Directories {
		if !validFinalDeletePath(dir) || seen[dir] {
			return false
		}
		seen[dir] = true
	}
	if snapshot.Entry.BeforeInventory.Kind == "file" {
		return len(snapshot.Files) == 1 && snapshot.Files[0].Path == "payload" && snapshot.Files[0].Size == snapshot.Entry.BeforeInventory.TotalSize && snapshot.Files[0].SHA256 == snapshot.Entry.BeforeInventory.SHA256 && len(snapshot.Directories) == 0
	}
	return len(snapshot.Directories) > 0 && snapshot.Directories[0] == "payload"
}

func finalDeleteSubset(files []TrashFinalDeleteFile, dirs []string, plannedFiles []TrashFinalDeleteFile, plannedDirs []string, partial bool) bool {
	if !partial {
		return reflect.DeepEqual(files, plannedFiles) && reflect.DeepEqual(dirs, plannedDirs)
	}
	wantFiles := make(map[string]TrashFinalDeleteFile, len(plannedFiles))
	for _, file := range plannedFiles {
		wantFiles[file.Path] = file
	}
	for _, file := range files {
		if wantFiles[file.Path] != file {
			return false
		}
	}
	wantDirs := make(map[string]bool, len(plannedDirs))
	for _, dir := range plannedDirs {
		wantDirs[dir] = true
	}
	for _, dir := range dirs {
		if !wantDirs[dir] {
			return false
		}
	}
	return true
}

func sameFinalDeleteIdentity(actual, planned models.TrashEntry) bool {
	actual.Status, planned.Status = "", ""
	actual.RestoreAvailable, planned.RestoreAvailable = false, false
	actual.LastCheckedAt, planned.LastCheckedAt = nil, nil
	actual.FinalDeletePlanID, planned.FinalDeletePlanID = "", ""
	actual.FinalDeleteOperationID, planned.FinalDeleteOperationID = "", ""
	actual.FinalDeleteFilesDeleted, planned.FinalDeleteFilesDeleted = 0, 0
	actual.FinalDeleteFilesTotal, planned.FinalDeleteFilesTotal = 0, 0
	actual.FinalDeletedAt, planned.FinalDeletedAt = nil, nil
	return reflect.DeepEqual(actual, planned)
}

func (r TrashFinalDeleteResult) String() string {
	return fmt.Sprintf("%s: %d/%d files %s", r.EntryID, r.DeletedFiles, r.TotalFiles, r.Status)
}
