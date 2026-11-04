package repositories

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/models"
)

type TrashRestorePreview struct {
	EntryID               string `json:"entry_id"`
	OriginalID            string `json:"original_id"`
	OriginalRelativePath  string `json:"original_relative_path"`
	ExpectedInventoryHash string `json:"expected_inventory_hash"`
	PathAvailable         bool   `json:"path_available"`
	IDAvailable           bool   `json:"id_available"`
	RestoreAvailable      bool   `json:"restore_available"`
	Reason                string `json:"reason,omitempty"`
}

type TrashRestoreStage string

const (
	TrashRestoreBeforeRename TrashRestoreStage = "before_rename"
	TrashRestoreAfterRename  TrashRestoreStage = "after_rename"
	TrashRestoreReadback     TrashRestoreStage = "readback"
	TrashRestoreSeal         TrashRestoreStage = "seal"
)

type TrashRestoreError struct {
	Stage       TrashRestoreStage
	SourceMoved bool
	Err         error
}

func (e *TrashRestoreError) Error() string {
	return fmt.Sprintf("Trash restore %s (source_moved=%t): %v", e.Stage, e.SourceMoved, e.Err)
}
func (e *TrashRestoreError) Unwrap() error { return e.Err }

func (r *TrashRepository) PreviewRestore(id string) (TrashRestorePreview, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return TrashRestorePreview{}, ErrMaintenanceID
	}
	var preview TrashRestorePreview
	err := r.withLock(func() error { var err error; preview, err = r.previewRestoreLocked(id); return err })
	return preview, err
}

func (r *TrashRepository) previewRestoreLocked(id string) (TrashRestorePreview, error) {
	entry, err := r.loadLocked(id)
	if err != nil {
		return TrashRestorePreview{}, err
	}
	p := TrashRestorePreview{EntryID: id, OriginalID: entry.OriginalID, OriginalRelativePath: entry.OriginalRelativePath}
	if entry.Status != "sealed" {
		p.Reason = "entry_not_sealed"
		return p, nil
	}
	payload := filepath.Join(r.config.Dir, id, "payload")
	if err := rejectSymlinkAncestors(payload); err != nil {
		p.Reason = "unsafe_payload"
		return p, nil
	}
	inv, err := trashInventory(payload)
	if err != nil {
		p.Reason = "payload_unavailable"
		return p, nil
	}
	p.ExpectedInventoryHash, err = restoreInventoryHash(entry, inv)
	if err != nil {
		return p, err
	}
	if inv != entry.BeforeInventory {
		p.Reason = "inventory_changed"
		return p, nil
	}
	target := filepath.Join(r.config.RuntimeDataDir, entry.OriginalRelativePath)
	if err := rejectSymlinkAncestors(filepath.Dir(target)); err != nil {
		p.Reason = "unsafe_target"
		return p, nil
	}
	if info, err := os.Stat(filepath.Dir(target)); err != nil || !info.IsDir() {
		p.Reason = "target_parent_unavailable"
		return p, nil
	}
	_, err = os.Lstat(target)
	if err == nil {
		p.Reason = "path_collision"
	} else if !errors.Is(err, os.ErrNotExist) {
		p.Reason = "target_unavailable"
		return p, nil
	} else {
		p.PathAvailable = true
	}
	p.IDAvailable, err = r.restoreIDAvailable(entry)
	if err != nil {
		p.Reason = "id_check_unavailable"
		return p, nil
	}
	if !p.IDAvailable {
		if p.Reason == "" {
			p.Reason = "id_collision"
		}
		return p, nil
	}
	if !p.PathAvailable {
		return p, nil
	}
	parent, err := os.Stat(filepath.Dir(target))
	if err != nil {
		return p, err
	}
	info, err := os.Lstat(payload)
	if err != nil {
		return p, err
	}
	if !r.sameDevice(info, parent) {
		p.Reason = "cross_device"
		return p, nil
	}
	p.RestoreAvailable = true
	return p, nil
}

func restoreInventoryHash(entry models.TrashEntry, inv models.TrashInventory) (string, error) {
	data, err := json.Marshal(struct {
		ID                   string                `json:"id"`
		OriginalID           string                `json:"original_id"`
		OriginalRelativePath string                `json:"original_relative_path"`
		TargetType           string                `json:"target_type"`
		Reason               string                `json:"reason"`
		OperationType        string                `json:"operation_type"`
		OperationID          string                `json:"operation_id"`
		DeletedAt            any                   `json:"deleted_at"`
		EligibleAfter        any                   `json:"eligible_after"`
		Inventory            models.TrashInventory `json:"inventory"`
	}{entry.ID, entry.OriginalID, entry.OriginalRelativePath, entry.TargetType, entry.Reason, entry.OperationType, entry.OperationID, entry.DeletedAt, entry.EligibleAfter, inv})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ID uniqueness is scoped to the original collection, not the whole runtime tree.
// Unknown layouts fail closed: a path vacancy alone is not proof of ID vacancy.
func (r *TrashRepository) restoreIDAvailable(entry models.TrashEntry) (bool, error) {
	rel := filepath.ToSlash(entry.OriginalRelativePath)
	parts := strings.Split(rel, "/")
	var collection string
	allowExistingContainer := false
	switch entry.TargetType {
	case "room":
		if parts[0] != "rooms" || !((len(parts) == 2 && parts[1] == entry.OriginalID) || (len(parts) == 3 && parts[1] == entry.OriginalID && parts[2] == "room.json")) {
			return false, ErrTrashBoundary
		}
		collection = "rooms"
		allowExistingContainer = len(parts) == 3
	case "device":
		if len(parts) < 4 || len(parts) > 5 || parts[0] != "rooms" || parts[2] != "devices" || parts[3] != entry.OriginalID || (len(parts) == 5 && parts[4] != "device.json") {
			return false, ErrTrashBoundary
		}
		collection = filepath.Join(parts[:3]...)
		allowExistingContainer = len(parts) == 5
	case "ir_command", "command":
		if len(parts) != 8 || parts[0] != "rooms" || parts[2] != "devices" || parts[4] != "adapters" || parts[5] != "ir" || parts[6] != "commands" || parts[7] != entry.OriginalID+".json" {
			return false, ErrTrashBoundary
		}
		collection = filepath.Join(parts[:7]...)
	case "ir_state", "state":
		if len(parts) != 8 || parts[0] != "rooms" || parts[2] != "devices" || parts[4] != "adapters" || parts[5] != "ir" || parts[6] != "states" || parts[7] != entry.OriginalID+".json" {
			return false, ErrTrashBoundary
		}
		collection = filepath.Join(parts[:7]...)
	default:
		base := filepath.Base(rel)
		if base != entry.OriginalID && base != entry.OriginalID+".json" {
			return false, ErrTrashBoundary
		}
		collection = filepath.Dir(rel)
	}
	path := filepath.Join(r.config.RuntimeDataDir, collection)
	if err := rejectSymlinkAncestors(path); err != nil {
		return false, err
	}
	items, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if allowExistingContainer && item.Name() == entry.OriginalID && item.IsDir() {
			continue // JSON-only deletion leaves its own parent directory in place.
		}
		if item.Name() == entry.OriginalID || item.Name() == entry.OriginalID+".json" {
			return false, nil
		}
	}
	return true, nil
}

func (r *TrashRepository) Restore(id, expectedHash, operationID string) (models.TrashEntry, error) {
	if !maintenanceIDPattern.MatchString(id) || !maintenanceIDPattern.MatchString(operationID) {
		return models.TrashEntry{}, ErrMaintenanceID
	}
	var result models.TrashEntry
	err := r.withLock(func() error {
		preview, err := r.previewRestoreLocked(id)
		if err != nil {
			return err
		}
		if !preview.RestoreAvailable || preview.ExpectedInventoryHash != expectedHash {
			return ErrTrashChanged
		}
		entry, err := r.loadLocked(id)
		if err != nil {
			return err
		}
		target := filepath.Join(r.config.RuntimeDataDir, entry.OriginalRelativePath)
		payload := filepath.Join(r.config.Dir, id, "payload")
		if err := r.injectRestore(TrashRestoreBeforeRename); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreBeforeRename, Err: err}
		}
		// Repeat the checks after the injection boundary and immediately before rename.
		preview, err = r.previewRestoreLocked(id)
		if err != nil {
			return err
		}
		if !preview.RestoreAvailable || preview.ExpectedInventoryHash != expectedHash {
			return ErrTrashChanged
		}
		entry.Status = "restoring"
		entry.RestoreAvailable = false
		entry.RestoreOperationID = operationID
		meta := filepath.Join(r.config.Dir, id, "metadata.json")
		if err := r.writer.WriteJSON(meta, entry, 0600); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreBeforeRename, Err: err}
		}
		if err := r.rename(payload, target); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreBeforeRename, Err: err}
		}
		if err := r.injectRestore(TrashRestoreAfterRename); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreAfterRename, SourceMoved: true, Err: err}
		}
		if err := errors.Join(r.syncDir(filepath.Dir(target)), r.syncDir(filepath.Dir(payload))); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreAfterRename, SourceMoved: true, Err: err}
		}
		if err := r.injectRestore(TrashRestoreReadback); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreReadback, SourceMoved: true, Err: err}
		}
		matches, missing := inspectTrashLocation(target, entry.BeforeInventory)
		_, payloadMissing := inspectTrashLocation(payload, entry.BeforeInventory)
		if !matches || missing || !payloadMissing {
			return &TrashRestoreError{Stage: TrashRestoreReadback, SourceMoved: true, Err: ErrTrashChanged}
		}
		if err := r.injectRestore(TrashRestoreSeal); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreSeal, SourceMoved: true, Err: err}
		}
		now := r.now()
		entry.Status = "restored"
		entry.RestoredAt = &now
		if err := r.writer.WriteJSON(meta, entry, 0600); err != nil {
			return &TrashRestoreError{Stage: TrashRestoreSeal, SourceMoved: true, Err: err}
		}
		result = entry
		return nil
	})
	return result, err
}

func (r *TrashRepository) injectRestore(stage TrashRestoreStage) error {
	if r.beforeRestoreStage != nil {
		return r.beforeRestoreStage(stage)
	}
	return nil
}
