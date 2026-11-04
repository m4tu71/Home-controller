package repositories

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"backend/models"
)

var (
	ErrTrashBoundary    = errors.New("invalid Trash boundary")
	ErrTrashCollision   = errors.New("Trash entry already exists")
	ErrTrashCrossDevice = errors.New("Trash and source are on different devices")
	ErrTrashChanged     = errors.New("Trash source or payload changed")
	ErrTrashMissing     = errors.New("Trash entry missing")
	ErrTrashCorrupt     = errors.New("corrupt Trash metadata")
)

type TrashConfig struct {
	Dir            string
	RuntimeDataDir string
	RepositoryDir  string
	WebRootDir     string
	StateDir       string
	BackupDir      string
	RetentionDays  int
}

type TrashMoveRequest struct {
	EntryID            string
	SourceRelativePath string
	OriginalID         string
	TargetType         string
	Reason             string
	OperationType      string
	OperationID        string
}

type TrashMoveStage string

const (
	TrashStageEntryCreate   TrashMoveStage = "entry_create"
	TrashStagePreparedWrite TrashMoveStage = "prepared_write"
	TrashStageBeforeRename  TrashMoveStage = "before_rename"
	TrashStageRename        TrashMoveStage = "rename"
	TrashStageAfterRename   TrashMoveStage = "after_rename"
	TrashStageParentSync    TrashMoveStage = "parent_sync"
	TrashStageReadback      TrashMoveStage = "readback"
	TrashStageSeal          TrashMoveStage = "seal"
)

type TrashMoveError struct {
	Stage        TrashMoveStage
	EntryCreated bool
	SourceMoved  bool
	Err          error
}

func (e *TrashMoveError) Error() string {
	return fmt.Sprintf("Trash move %s (entry_created=%t source_moved=%t): %v", e.Stage, e.EntryCreated, e.SourceMoved, e.Err)
}
func (e *TrashMoveError) Unwrap() error { return e.Err }

type TrashMoveResult struct {
	EntryID      string
	EntryCreated bool
	SourceMoved  bool
	Verified     bool
	Entry        models.TrashEntry
}

type TrashRepository struct {
	config             TrashConfig
	uid                uint32
	writer             JSONWriter
	now                func() time.Time
	rename             func(string, string) error
	syncDir            func(string) error
	beforeStage        func(TrashMoveStage) error
	beforeRestoreStage func(TrashRestoreStage) error
	sameDevice         func(os.FileInfo, os.FileInfo) bool
}

// List reads metadata only. One malformed entry makes the whole list fail
// closed; it never silently hides a potentially recoverable Trash object.
func (r *TrashRepository) List() ([]models.TrashEntry, error) {
	entries := []models.TrashEntry{}
	err := r.withLock(func() error {
		items, err := os.ReadDir(r.config.Dir)
		if err != nil {
			return err
		}
		for _, item := range items {
			id := item.Name()
			if !maintenanceIDPattern.MatchString(id) {
				return fmt.Errorf("%w: unexpected Trash entry", ErrTrashCorrupt)
			}
			entry, err := r.loadLocked(id)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.DeletedAt == nil {
			return b.DeletedAt == nil && a.ID < b.ID
		}
		if b.DeletedAt == nil {
			return true
		}
		if a.DeletedAt.Equal(*b.DeletedAt) {
			return a.ID < b.ID
		}
		return a.DeletedAt.After(*b.DeletedAt)
	})
	return entries, nil
}

func NewTrashRepositoryFromEnv() (*TrashRepository, error) {
	return NewTrashRepositoryFromEnvWithRuntimeDataDir(filepath.Dir(defaultRoomsDir))
}

// NewTrashRepositoryFromEnvWithRuntimeDataDir keeps the installation's
// Maintenance/Trash roots from the legacy environment during the transition,
// while allowing ordinary CRUD deletion to resolve its source under the
// configured runtime data root.
func NewTrashRepositoryFromEnvWithRuntimeDataDir(runtimeDataDir string) (*TrashRepository, error) {
	days, err := strconv.Atoi(os.Getenv("WEB_CONTROLLER_TRASH_RETENTION_DAYS"))
	if err != nil {
		return nil, fmt.Errorf("%w: WEB_CONTROLLER_TRASH_RETENTION_DAYS must be a positive integer", ErrTrashBoundary)
	}
	return NewTrashRepository(TrashConfig{Dir: os.Getenv("WEB_CONTROLLER_TRASH_DIR"), RuntimeDataDir: runtimeDataDir, RepositoryDir: filepath.Dir(filepath.Dir(defaultRoomsDir)), WebRootDir: "/var/www/web-controller", StateDir: os.Getenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR"), BackupDir: os.Getenv("WEB_CONTROLLER_BACKUP_DIR"), RetentionDays: days})
}

func NewTrashRepository(config TrashConfig) (*TrashRepository, error) {
	r := &TrashRepository{config: config, uid: uint32(os.Geteuid()), writer: NewAtomicJSONWriter(), now: func() time.Time { return time.Now().UTC() }, rename: trashRenameNoReplace, syncDir: syncTrashDirectory, sameDevice: trashSameDevice}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *TrashRepository) validate() error {
	if r == nil || r.config.Dir == "" || !filepath.IsAbs(r.config.Dir) || r.config.RuntimeDataDir == "" || !filepath.IsAbs(r.config.RuntimeDataDir) || r.config.StateDir == "" || !filepath.IsAbs(r.config.StateDir) || r.config.RetentionDays <= 0 {
		return fmt.Errorf("%w: absolute Trash/runtime/state roots and positive retention required", ErrTrashBoundary)
	}
	root := filepath.Clean(r.config.Dir)
	for _, protected := range []string{r.config.RuntimeDataDir, r.config.StateDir, r.config.RepositoryDir, r.config.WebRootDir, r.config.BackupDir} {
		if protected == "" {
			continue
		}
		if !filepath.IsAbs(protected) {
			return fmt.Errorf("%w: relative protected root", ErrTrashBoundary)
		}
		p := filepath.Clean(protected)
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if containsPath(root, p) || containsPath(p, root) {
			return fmt.Errorf("%w: Trash overlaps protected root", ErrTrashBoundary)
		}
	}
	if err := validateDirectory(root, r.uid); err != nil {
		return fmt.Errorf("%w: %v", ErrTrashBoundary, err)
	}
	if err := rejectSymlinkAncestors(filepath.Clean(r.config.RuntimeDataDir)); err != nil {
		return fmt.Errorf("%w: runtime root: %v", ErrTrashBoundary, err)
	}
	return nil
}

func validTrashRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\\:") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (r *TrashRepository) withLock(fn func() error) error {
	if err := r.validate(); err != nil {
		return err
	}
	f, err := os.Open(r.config.Dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := r.validate(); err != nil {
		return err
	}
	return fn()
}

func (r *TrashRepository) Move(request TrashMoveRequest) (result TrashMoveResult, resultErr error) {
	result.EntryID = request.EntryID
	if !maintenanceIDPattern.MatchString(request.EntryID) || !maintenanceIDPattern.MatchString(request.OperationID) || !validTrashRelative(request.SourceRelativePath) || request.OriginalID == "" || request.TargetType == "" || request.Reason == "" || request.OperationType == "" {
		return result, fmt.Errorf("%w: invalid move request", ErrTrashBoundary)
	}
	err := r.withLock(func() error { return r.moveLocked(request, &result) })
	return result, err
}

func (r *TrashRepository) moveLocked(request TrashMoveRequest, result *TrashMoveResult) error {
	entryDir := filepath.Join(r.config.Dir, request.EntryID)
	if _, err := os.Lstat(entryDir); err == nil {
		return &TrashMoveError{Stage: TrashStageEntryCreate, Err: ErrTrashCollision}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &TrashMoveError{Stage: TrashStageEntryCreate, Err: err}
	}
	source := filepath.Join(r.config.RuntimeDataDir, request.SourceRelativePath)
	if err := rejectSymlinkAncestors(source); err != nil {
		return &TrashMoveError{Stage: TrashStageBeforeRename, Err: err}
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return &TrashMoveError{Stage: TrashStageBeforeRename, Err: err}
	}
	rootInfo, err := os.Stat(r.config.Dir)
	if err != nil {
		return &TrashMoveError{Stage: TrashStageBeforeRename, Err: err}
	}
	if !r.sameDevice(sourceInfo, rootInfo) {
		return &TrashMoveError{Stage: TrashStageBeforeRename, Err: ErrTrashCrossDevice}
	}
	before, err := trashInventory(source)
	if err != nil {
		return &TrashMoveError{Stage: TrashStageBeforeRename, Err: err}
	}
	if err := os.Mkdir(entryDir, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = ErrTrashCollision
		}
		return &TrashMoveError{Stage: TrashStageEntryCreate, Err: err}
	}
	result.EntryCreated = true
	if err := os.Chmod(entryDir, 0700); err != nil {
		return r.moveError(TrashStageEntryCreate, result, err)
	}
	if err := r.syncDir(r.config.Dir); err != nil {
		return r.moveError(TrashStageEntryCreate, result, err)
	}
	entry := models.TrashEntry{Version: 1, ID: request.EntryID, Status: "prepared", OriginalRelativePath: request.SourceRelativePath, OriginalID: request.OriginalID, TargetType: request.TargetType, Reason: request.Reason, OperationType: request.OperationType, OperationID: request.OperationID, BeforeInventory: before}
	result.Entry = entry
	metadataPath := filepath.Join(entryDir, "metadata.json")
	if err := r.writer.WriteJSON(metadataPath, entry, 0600); err != nil {
		return r.moveError(TrashStagePreparedWrite, result, err)
	}
	if err := r.inject(TrashStageBeforeRename); err != nil {
		return r.moveError(TrashStageBeforeRename, result, err)
	}
	if err := rejectSymlinkAncestors(source); err != nil {
		return r.moveError(TrashStageBeforeRename, result, err)
	}
	latest, err := trashInventory(source)
	if err != nil || latest != before {
		return r.moveError(TrashStageBeforeRename, result, errors.Join(ErrTrashChanged, err))
	}
	payload := filepath.Join(entryDir, "payload")
	if _, err := os.Lstat(payload); err == nil {
		return r.moveError(TrashStageBeforeRename, result, ErrTrashCollision)
	} else if !errors.Is(err, os.ErrNotExist) {
		return r.moveError(TrashStageBeforeRename, result, err)
	}
	if err := r.rename(source, payload); err != nil {
		return r.moveError(TrashStageRename, result, err)
	}
	result.SourceMoved = true
	if err := r.inject(TrashStageAfterRename); err != nil {
		return r.moveError(TrashStageAfterRename, result, err)
	}
	syncErr := errors.Join(r.syncDir(filepath.Dir(source)), r.syncDir(entryDir))
	if syncErr != nil {
		return r.moveError(TrashStageParentSync, result, syncErr)
	}
	if err := r.inject(TrashStageReadback); err != nil {
		return r.moveError(TrashStageReadback, result, err)
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		return r.moveError(TrashStageReadback, result, errors.Join(ErrTrashChanged, err))
	}
	after, err := trashInventory(payload)
	if err != nil || after != before {
		return r.moveError(TrashStageReadback, result, errors.Join(ErrTrashChanged, err))
	}
	if err := r.inject(TrashStageSeal); err != nil {
		return r.moveError(TrashStageSeal, result, err)
	}
	deletedAt := r.now()
	eligibleAfter := deletedAt.AddDate(0, 0, r.config.RetentionDays)
	entry.Status = "sealed"
	entry.DeletedAt = &deletedAt
	entry.EligibleAfter = &eligibleAfter
	entry.RestoreAvailable = true
	if err := r.writer.WriteJSON(metadataPath, entry, 0600); err != nil {
		return r.moveError(TrashStageSeal, result, err)
	}
	result.Entry = entry
	result.Verified = true
	return nil
}

func (r *TrashRepository) inject(stage TrashMoveStage) error {
	if r.beforeStage != nil {
		return r.beforeStage(stage)
	}
	return nil
}
func (r *TrashRepository) moveError(stage TrashMoveStage, result *TrashMoveResult, err error) error {
	return &TrashMoveError{Stage: stage, EntryCreated: result.EntryCreated, SourceMoved: result.SourceMoved, Err: err}
}

func syncTrashDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	return errors.Join(syncErr, f.Close())
}

func trashSameDevice(source, root os.FileInfo) bool {
	srcStat, srcOK := source.Sys().(*syscall.Stat_t)
	rootStat, rootOK := root.Sys().(*syscall.Stat_t)
	return srcOK && rootOK && srcStat.Dev == rootStat.Dev
}

func (r *TrashRepository) Load(id string) (models.TrashEntry, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return models.TrashEntry{}, ErrMaintenanceID
	}
	var entry models.TrashEntry
	err := r.withLock(func() error { var err error; entry, err = r.loadLocked(id); return err })
	return entry, err
}

func (r *TrashRepository) loadLocked(id string) (models.TrashEntry, error) {
	dir := filepath.Join(r.config.Dir, id)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return models.TrashEntry{}, ErrTrashMissing
	} else if err != nil {
		return models.TrashEntry{}, err
	}
	if err := validateDirectory(dir, r.uid); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return models.TrashEntry{}, ErrTrashMissing
		}
		return models.TrashEntry{}, fmt.Errorf("%w: entry directory: %v", ErrTrashBoundary, err)
	}
	path := filepath.Join(dir, "metadata.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return models.TrashEntry{}, ErrTrashCorrupt
	}
	if err != nil {
		return models.TrashEntry{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != r.uid || info.Mode().Perm() != 0600 {
		return models.TrashEntry{}, ErrTrashBoundary
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return models.TrashEntry{}, ErrTrashBoundary
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return models.TrashEntry{}, ErrTrashBoundary
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil {
		return models.TrashEntry{}, err
	}
	if len(data) > 1<<20 {
		return models.TrashEntry{}, ErrTrashCorrupt
	}
	var entry models.TrashEntry
	if json.Unmarshal(data, &entry) != nil || !validTrashEntry(entry, id) {
		return models.TrashEntry{}, ErrTrashCorrupt
	}
	return entry, nil
}

func validTrashEntry(entry models.TrashEntry, id string) bool {
	if entry.Version != 1 || entry.ID != id || !validTrashRelative(entry.OriginalRelativePath) || entry.OriginalID == "" || entry.TargetType == "" || entry.Reason == "" || entry.OperationType == "" || !maintenanceIDPattern.MatchString(entry.OperationID) {
		return false
	}
	inv := entry.BeforeInventory
	if (inv.Kind != "file" && inv.Kind != "directory") || inv.FileCount < 0 || inv.TotalSize < 0 || len(inv.SHA256) != 64 {
		return false
	}
	if _, err := hex.DecodeString(inv.SHA256); err != nil {
		return false
	}
	if entry.LastCheckedAt != nil && (entry.DeletedAt == nil || entry.LastCheckedAt.Before(*entry.DeletedAt)) {
		return false
	}
	if entry.Status == "prepared" {
		return entry.DeletedAt == nil && entry.EligibleAfter == nil && entry.LastCheckedAt == nil && !entry.RestoreAvailable
	}
	if entry.Status == "sealed" {
		return entry.DeletedAt != nil && entry.EligibleAfter != nil && entry.EligibleAfter.After(*entry.DeletedAt) && entry.RestoreAvailable && entry.RestoreOperationID == "" && entry.RestoredAt == nil && entry.FinalDeletePlanID == "" && entry.FinalDeleteOperationID == "" && entry.FinalDeleteFilesDeleted == 0 && entry.FinalDeleteFilesTotal == 0 && entry.FinalDeletedAt == nil
	}
	if entry.Status == "final_deleting" || entry.Status == "final_deleted" {
		if entry.DeletedAt == nil || entry.EligibleAfter == nil || entry.LastCheckedAt == nil || !maintenanceIDPattern.MatchString(entry.FinalDeletePlanID) || !maintenanceIDPattern.MatchString(entry.FinalDeleteOperationID) || entry.RestoreAvailable || entry.RestoreOperationID != "" || entry.FinalDeleteFilesDeleted < 0 || entry.FinalDeleteFilesDeleted > entry.FinalDeleteFilesTotal || (entry.Status == "final_deleted") != (entry.FinalDeletedAt != nil) {
			return false
		}
		return entry.Status != "final_deleted" || (entry.FinalDeleteFilesDeleted == entry.FinalDeleteFilesTotal && !entry.FinalDeletedAt.Before(*entry.EligibleAfter))
	}
	if entry.Status == "restoring" || entry.Status == "restored" {
		return entry.DeletedAt != nil && entry.EligibleAfter != nil && maintenanceIDPattern.MatchString(entry.RestoreOperationID) && !entry.RestoreAvailable && (entry.Status == "restored") == (entry.RestoredAt != nil)
	}
	return false
}

// Reconcile observes a prepared or sealed entry without changing either tree.
func (r *TrashRepository) Reconcile(id string) (string, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return "", ErrMaintenanceID
	}
	classification := "ambiguous"
	err := r.withLock(func() error {
		entry, err := r.loadLocked(id)
		if err != nil {
			return err
		}
		source := filepath.Join(r.config.RuntimeDataDir, entry.OriginalRelativePath)
		payload := filepath.Join(r.config.Dir, id, "payload")
		sourceMatches, sourceMissing := inspectTrashLocation(source, entry.BeforeInventory)
		payloadMatches, payloadMissing := inspectTrashLocation(payload, entry.BeforeInventory)
		if sourceMatches && payloadMissing && (entry.Status == "restoring" || entry.Status == "restored") {
			classification = "restored_unconfirmed"
		} else if sourceMatches && payloadMissing {
			classification = "unchanged"
		} else if sourceMissing && payloadMatches {
			classification = "moved_unconfirmed"
		}
		return nil
	})
	return classification, err
}

func inspectTrashLocation(path string, expected models.TrashInventory) (matches, missing bool) {
	if err := rejectSymlinkAncestors(filepath.Dir(path)); err != nil {
		return false, false
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, true
	} else if err != nil {
		return false, false
	}
	if err := rejectSymlinkAncestors(path); err != nil {
		return false, false
	}
	inv, err := trashInventory(path)
	return err == nil && inv == expected, false
}
