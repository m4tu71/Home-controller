package repositories

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/models"
)

type BackupRestoreStage string

const (
	BackupRestoreStagePrepare            BackupRestoreStage = "prepare"
	BackupRestoreStageCopy               BackupRestoreStage = "copy"
	BackupRestoreStageReadback           BackupRestoreStage = "readback"
	BackupRestoreStageRepositoryReadback BackupRestoreStage = "repository_readback"
	BackupRestoreStagePromote            BackupRestoreStage = "promote"
)

type BackupRestorePreview struct {
	BackupID            string `json:"backup_id"`
	Role                string `json:"role"`
	State               string `json:"state"`
	Target              string `json:"target"`
	RequiredBytes       uint64 `json:"required_bytes"`
	AvailableBytes      uint64 `json:"available_bytes"`
	SourceInventoryHash string `json:"source_inventory_hash"`
	TargetInventoryHash string `json:"target_inventory_hash,omitempty"`
	TargetExists        bool   `json:"target_exists"`
	CanRehearse         bool   `json:"can_rehearse"`
	Reason              string `json:"reason,omitempty"`
}

type BackupRestoreResult struct {
	BackupID      string `json:"backup_id"`
	Role          string `json:"role"`
	PreviousState string `json:"previous_state"`
	State         string `json:"state"`
	Target        string `json:"target"`
	InventoryHash string `json:"inventory_hash"`
	FileCount     int64  `json:"file_count"`
	TotalSize     int64  `json:"total_size"`
}

type BackupRestoreError struct {
	Stage BackupRestoreStage
	ID    string
	Err   error
}

func (e *BackupRestoreError) Error() string {
	return fmt.Sprintf("backup restore %s (%s): %v", e.Stage, e.ID, e.Err)
}
func (e *BackupRestoreError) Unwrap() error { return e.Err }

var ErrBackupRestoreConflict = errors.New("backup restore is not safe")

func (r *BackupRepository) restoreTarget(id string) string {
	return filepath.Join(r.config.Dir, ".restore-rehearsal", id)
}
func (r *BackupRepository) restoreTargetLabel(id string) string { return "isolated-rehearsal/" + id }

func (r *BackupRepository) RestoreDryRun(id string) (BackupRestorePreview, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return BackupRestorePreview{}, ErrMaintenanceID
	}
	var preview BackupRestorePreview
	err := r.withLock(func() error { var err error; preview, err = r.restoreDryRunLocked(id); return err })
	return preview, err
}

func (r *BackupRepository) restoreDryRunLocked(id string) (BackupRestorePreview, error) {
	detail, err := r.loadLocked(id)
	if err != nil {
		return BackupRestorePreview{}, err
	}
	b, registry := detail.Bundle, detail.Registry
	p := BackupRestorePreview{BackupID: id, Role: registry.Role, State: registry.State, Target: r.restoreTargetLabel(id), SourceInventoryHash: b.InventoryHash, RequiredBytes: uint64(b.TotalSize) + uint64(len(b.Files)+len(b.Directories)+1)*4096 + (1 << 20)}
	if registry.Role != "rehearsal" {
		p.Reason = "wrong_backup_role"
		return p, nil
	}
	if registry.State != "snapshot_verified" {
		p.Reason = "backup_not_rehearsal_candidate"
		return p, nil
	}
	free, err := r.available(r.config.Dir)
	if err != nil {
		return p, err
	}
	p.AvailableBytes = free
	target := r.restoreTarget(id)
	parent := filepath.Dir(target)
	if err := rejectBackupRestoreSymlink(filepath.Join(parent, id)); err != nil {
		p.Reason = "unsafe_target"
		return p, nil
	}
	if info, err := os.Lstat(target); err == nil {
		p.TargetExists = true
		if info.Mode()&os.ModeSymlink != 0 {
			p.Reason = "unsafe_target"
			return p, nil
		}
		actual, inventoryErr := r.inventory(target, b.Sources)
		if inventoryErr == nil {
			p.TargetInventoryHash = actual.InventoryHash
		}
		p.Reason = "target_collision"
		return p, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	if _, err := os.Lstat(parent); err == nil {
		if err := validateDirectory(parent, r.uid); err != nil {
			p.Reason = "unsafe_target"
			return p, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	if free < p.RequiredBytes {
		p.Reason = "insufficient_capacity"
		return p, nil
	}
	p.CanRehearse = true
	return p, nil
}

func rejectBackupRestoreSymlink(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if current == filepath.Dir(current) {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrBackupBoundary
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func (r *BackupRepository) RestoreRehearsal(id string) (BackupRestoreResult, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return BackupRestoreResult{}, ErrMaintenanceID
	}
	var result BackupRestoreResult
	err := r.withLock(func() error { return r.restoreRehearsalLocked(id, &result) })
	return result, err
}

func (r *BackupRepository) restoreRehearsalLocked(id string, result *BackupRestoreResult) error {
	p, err := r.restoreDryRunLocked(id)
	if err != nil {
		return err
	}
	if !p.CanRehearse {
		return ErrBackupRestoreConflict
	}
	detail, err := r.loadLocked(id)
	if err != nil {
		return err
	}
	bundle := detail.Bundle
	if err := r.injectRestore(BackupRestoreStagePrepare); err != nil {
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	parent := filepath.Dir(r.restoreTarget(id))
	_, parentStatErr := os.Lstat(parent)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	if err := os.Chmod(parent, 0700); err != nil {
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	if err := validateDirectory(parent, r.uid); err != nil {
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	if errors.Is(parentStatErr, os.ErrNotExist) {
		if err := r.syncRoot(); err != nil {
			return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
		}
	}
	target := r.restoreTarget(id)
	if err := os.Mkdir(target, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = errors.Join(ErrBackupRestoreConflict, err)
		}
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	if err := syncTrashDirectory(parent); err != nil {
		return &BackupRestoreError{BackupRestoreStagePrepare, id, err}
	}
	if err := r.injectRestore(BackupRestoreStageCopy); err != nil {
		return &BackupRestoreError{BackupRestoreStageCopy, id, err}
	}
	dataRoot := filepath.Join(r.config.Dir, id, "data")
	inv := backupInventory{Files: bundle.Files, Directories: bundle.Directories, FileCount: bundle.FileCount, TotalSize: bundle.TotalSize, InventoryHash: bundle.InventoryHash}
	if err := r.copyInventoryFrom(dataRoot, inv, target); err != nil {
		return &BackupRestoreError{BackupRestoreStageCopy, id, err}
	}
	if err := r.injectRestore(BackupRestoreStageReadback); err != nil {
		return &BackupRestoreError{BackupRestoreStageReadback, id, err}
	}
	actual, err := r.inventory(target, bundle.Sources)
	if err != nil || !sameBackupInventory(inv, actual) {
		return &BackupRestoreError{BackupRestoreStageReadback, id, errors.Join(ErrBackupCorrupt, err)}
	}
	if err := checkBackupContents(target, actual); err != nil {
		return &BackupRestoreError{BackupRestoreStageReadback, id, err}
	}
	if err := r.injectRestore(BackupRestoreStageRepositoryReadback); err != nil {
		return &BackupRestoreError{BackupRestoreStageRepositoryReadback, id, err}
	}
	if err := verifyBackupJSONReadback(target, bundle.Files); err != nil {
		return &BackupRestoreError{BackupRestoreStageRepositoryReadback, id, err}
	}
	if r.restoreReadbackValidator != nil {
		if err := r.restoreReadbackValidator(target, bundle.Files); err != nil {
			return &BackupRestoreError{BackupRestoreStageRepositoryReadback, id, errors.Join(ErrBackupCorrupt, err)}
		}
	}
	afterDecode, err := r.inventory(target, bundle.Sources)
	if err != nil || !sameBackupInventory(inv, afterDecode) {
		return &BackupRestoreError{BackupRestoreStageRepositoryReadback, id, errors.Join(ErrBackupCorrupt, err)}
	}
	if err := checkBackupContents(target, afterDecode); err != nil {
		return &BackupRestoreError{BackupRestoreStageRepositoryReadback, id, err}
	}
	// Confirm the sealed source bundle did not change while the rehearsal copy ran.
	if _, err := r.loadLocked(id); err != nil {
		return &BackupRestoreError{BackupRestoreStageReadback, id, err}
	}
	if err := r.injectRestore(BackupRestoreStagePromote); err != nil {
		return &BackupRestoreError{BackupRestoreStagePromote, id, err}
	}
	registry := detail.Registry
	registry.State = "rehearsal_verified"
	registry.UpdatedAt = r.now()
	if err := r.writer.WriteJSON(filepath.Join(r.config.Dir, id, "registry.json"), registry, 0600); err != nil {
		return &BackupRestoreError{BackupRestoreStagePromote, id, err}
	}
	if err := r.syncRoot(); err != nil {
		return &BackupRestoreError{BackupRestoreStagePromote, id, err}
	}
	if _, err := r.loadLocked(id); err != nil {
		return &BackupRestoreError{BackupRestoreStagePromote, id, err}
	}
	*result = BackupRestoreResult{BackupID: id, Role: registry.Role, PreviousState: detail.Registry.State, State: registry.State, Target: r.restoreTargetLabel(id), InventoryHash: bundle.InventoryHash, FileCount: bundle.FileCount, TotalSize: bundle.TotalSize}
	return nil
}

// WithRestoreReadbackValidator installs service-owned semantic validation at startup.
// The repository keeps the isolated path private and runs it before promotion.
func (r *BackupRepository) WithRestoreReadbackValidator(validate func(string, []models.BackupFile) error) *BackupRepository {
	r.restoreReadbackValidator = validate
	return r
}

func verifyBackupJSONReadback(root string, files []models.BackupFile) error {
	for _, entry := range files {
		if !strings.EqualFold(filepath.Ext(entry.Path), ".json") || entry.JSONStatus != "valid" {
			continue
		}
		if err := readBackupJSONThroughRepository(root, entry.Path); err != nil {
			return errors.Join(ErrBackupCorrupt, fmt.Errorf("repository readback %s: %w", entry.Path, err))
		}
	}
	return nil
}

func readBackupJSONThroughRepository(root, relativePath string) error {
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	switch {
	case len(parts) == 3 && parts[0] == "rooms" && parts[2] == "room.json":
		_, err := NewRoomRepositoryWithBaseDir(filepath.Join(root, "rooms")).Load(parts[1])
		return err
	case len(parts) == 5 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "device.json":
		_, err := NewDeviceRepositoryWithRoomsDir(filepath.Join(root, "rooms")).Load(parts[1], parts[3])
		return err
	case len(parts) == 7 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "ir_config.json":
		_, err := NewIRAdapterRepositoryWithRoomsDir(filepath.Join(root, "rooms")).LoadConfig(parts[1], parts[3])
		return err
	case len(parts) == 7 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "state_schema.json":
		_, err := NewIRAdapterRepositoryWithRoomsDir(filepath.Join(root, "rooms")).LoadStateSchema(parts[1], parts[3])
		return err
	case len(parts) == 8 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "commands" && strings.HasSuffix(parts[7], ".json"):
		_, err := NewIRAdapterRepositoryWithRoomsDir(filepath.Join(root, "rooms")).LoadCommand(parts[1], parts[3], strings.TrimSuffix(parts[7], ".json"))
		return err
	case len(parts) == 8 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "states" && strings.HasSuffix(parts[7], ".json"):
		_, err := NewIRAdapterRepositoryWithRoomsDir(filepath.Join(root, "rooms")).LoadState(parts[1], parts[3], strings.TrimSuffix(parts[7], ".json"))
		return err
	case len(parts) == 2 && parts[0] == "ir-signals" && strings.HasSuffix(parts[1], ".json"):
		// Historical JSON still needs typed isolated-restore readback until Step 17.
		// This is not a runtime CRUD repository or an HTTP API.
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			return err
		}
		var source models.MigrationSourceSignal
		return json.Unmarshal(data, &source)
	default:
		return fmt.Errorf("no repository readback mapping for %s", relativePath)
	}
}

func (r *BackupRepository) injectRestore(stage BackupRestoreStage) error {
	if r.beforeRestoreStage != nil {
		return r.beforeRestoreStage(stage)
	}
	return nil
}
