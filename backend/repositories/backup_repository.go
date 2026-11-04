package repositories

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"backend/models"
)

var (
	ErrBackupBoundary = errors.New("invalid backup boundary")
	ErrBackupConflict = errors.New("backup collision or source changed")
	ErrBackupMissing  = errors.New("backup missing")
	ErrBackupCorrupt  = errors.New("backup bundle corrupt")
	ErrBackupCapacity = errors.New("insufficient backup capacity")
)

type BackupConfig struct {
	Dir            string
	RuntimeDataDir string
	RepositoryDir  string
	WebRootDir     string
	StateDir       string
	TrashDir       string
}

type BackupCreate struct {
	ID                    string
	Role                  string
	PlanID                string
	ActionIDs             []string
	Sources               []string // Runtime-root-relative paths from a trusted, verified plan.
	ExpectedInventoryHash string   // Optional digest captured in the saved plan.
}

type BackupStage string

const (
	BackupStageDraft    BackupStage = "draft"
	BackupStageCopy     BackupStage = "copy"
	BackupStageReadback BackupStage = "readback"
	BackupStageSeal     BackupStage = "seal"
)

type BackupError struct {
	Stage BackupStage
	ID    string
	Err   error
}

func (e *BackupError) Error() string { return fmt.Sprintf("backup %s (%s): %v", e.Stage, e.ID, e.Err) }
func (e *BackupError) Unwrap() error { return e.Err }

type BackupRepository struct {
	restoreReadbackValidator func(string, []models.BackupFile) error
	config                   BackupConfig
	uid                      uint32
	writer                   JSONWriter
	now                      func() time.Time
	available                func(string) (uint64, error)
	beforeStage              func(BackupStage) error
	beforeCopyFile           func(string) error
	beforeRestoreStage       func(BackupRestoreStage) error
}

func NewBackupRepositoryFromEnv() (*BackupRepository, error) {
	return NewBackupRepositoryFromEnvWithRuntimeDataDir(filepath.Dir(defaultRoomsDir))
}

// NewBackupRepositoryFromEnvWithRuntimeDataDir keeps the legacy environment
// contract while allowing callers to select the runtime data root explicitly.
func NewBackupRepositoryFromEnvWithRuntimeDataDir(runtimeDataDir string) (*BackupRepository, error) {
	return NewBackupRepository(BackupConfig{Dir: os.Getenv("WEB_CONTROLLER_BACKUP_DIR"), RuntimeDataDir: runtimeDataDir, RepositoryDir: filepath.Dir(filepath.Dir(defaultRoomsDir)), WebRootDir: "/var/www/web-controller", StateDir: os.Getenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR"), TrashDir: os.Getenv("WEB_CONTROLLER_TRASH_DIR")})
}

func NewBackupRepository(c BackupConfig) (*BackupRepository, error) {
	r := &BackupRepository{config: c, uid: uint32(os.Geteuid()), writer: NewAtomicJSONWriter(), now: func() time.Time { return time.Now().UTC() }, available: backupAvailableBytes}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if free, err := r.available(r.config.Dir); err != nil || free < 1<<20 {
		return nil, errors.Join(ErrBackupCapacity, err)
	}
	return r, nil
}

func (r *BackupRepository) validate() error {
	if r == nil || r.config.Dir == "" || r.config.RuntimeDataDir == "" || r.config.RepositoryDir == "" || r.config.WebRootDir == "" || r.config.StateDir == "" || r.config.TrashDir == "" || !filepath.IsAbs(r.config.Dir) || !filepath.IsAbs(r.config.RuntimeDataDir) {
		return ErrBackupBoundary
	}
	root := filepath.Clean(r.config.Dir)
	if err := validateDirectory(root, r.uid); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupBoundary, err)
	}
	if err := rejectSymlinkAncestors(r.config.RuntimeDataDir); err != nil {
		return fmt.Errorf("%w: source root: %v", ErrBackupBoundary, err)
	}
	for _, protected := range []string{r.config.RuntimeDataDir, r.config.RepositoryDir, r.config.WebRootDir, r.config.StateDir, r.config.TrashDir} {
		if protected == "" {
			continue
		}
		if !filepath.IsAbs(protected) {
			return ErrBackupBoundary
		}
		p := filepath.Clean(protected)
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if containsPath(root, p) || containsPath(p, root) {
			return fmt.Errorf("%w: backup root overlaps protected path", ErrBackupBoundary)
		}
	}
	return nil
}

func backupAvailableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func (r *BackupRepository) withLock(fn func() error) error {
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

func validBackupCreate(v BackupCreate) bool {
	if !maintenanceIDPattern.MatchString(v.ID) || (v.Role != "rehearsal" && v.Role != "apply") || len(v.Sources) == 0 || len(v.ActionIDs) == 0 {
		return false
	}
	if v.ExpectedInventoryHash != "" && !validBackupInventoryHash(v.ExpectedInventoryHash) {
		return false
	}
	if v.PlanID != "" && !maintenanceIDPattern.MatchString(v.PlanID) {
		return false
	}
	if v.Role == "rehearsal" && v.PlanID == "" {
		return false
	}
	seen := map[string]bool{}
	for _, id := range v.ActionIDs {
		if !maintenanceIDPattern.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return validBackupSources(v.Sources)
}

func validBackupSources(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if !validTrashRelative(source) || seen[source] {
			return false
		}
		seen[source] = true
	}
	for i, a := range sources {
		for j, b := range sources {
			if i != j && containsPath(a, b) {
				return false
			}
		}
	}
	return true
}

func validBackupInventoryHash(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func (r *BackupRepository) Create(v BackupCreate) (models.BackupDetail, error) {
	if !validBackupCreate(v) {
		return models.BackupDetail{}, ErrBackupBoundary
	}
	var detail models.BackupDetail
	err := r.withLock(func() error { var err error; detail, err = r.createLocked(v); return err })
	return detail, err
}

func (r *BackupRepository) createLocked(v BackupCreate) (models.BackupDetail, error) {
	var detail models.BackupDetail
	dir := filepath.Join(r.config.Dir, v.ID)
	if _, err := os.Lstat(dir); err == nil {
		return detail, ErrBackupConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return detail, err
	}
	before, err := r.inventory(r.config.RuntimeDataDir, v.Sources)
	if err != nil {
		return detail, err
	}
	if v.ExpectedInventoryHash != "" && before.InventoryHash != v.ExpectedInventoryHash {
		return detail, ErrBackupConflict
	}
	available, err := r.available(r.config.Dir)
	if err != nil {
		return detail, err
	}
	// Allow for metadata and filesystem bookkeeping in addition to content bytes.
	required := uint64(before.TotalSize) + uint64(len(before.Files)+len(before.Directories)+1)*4096 + (1 << 20)
	if available < required {
		return detail, ErrBackupCapacity
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return detail, err
	}
	if err := r.syncRoot(); err != nil {
		return detail, &BackupError{BackupStageDraft, v.ID, err}
	}
	created := r.now()
	draft := models.BackupRegistry{Version: 1, ID: v.ID, Role: v.Role, State: "draft", UpdatedAt: created}
	if err := r.writer.WriteJSON(filepath.Join(dir, "registry.json"), draft, 0600); err != nil {
		return detail, &BackupError{BackupStageDraft, v.ID, err}
	}
	if err := r.inject(BackupStageDraft); err != nil {
		return detail, &BackupError{BackupStageDraft, v.ID, err}
	}
	dataRoot := filepath.Join(dir, "data")
	if err := os.Mkdir(dataRoot, 0700); err != nil {
		return detail, &BackupError{BackupStageCopy, v.ID, err}
	}
	if err := r.inject(BackupStageCopy); err != nil {
		return detail, &BackupError{BackupStageCopy, v.ID, err}
	}
	if err := r.copyInventory(before, dataRoot); err != nil {
		return detail, &BackupError{BackupStageCopy, v.ID, err}
	}
	if err := r.inject(BackupStageReadback); err != nil {
		return detail, &BackupError{BackupStageReadback, v.ID, err}
	}
	after, err := r.inventory(dataRoot, v.Sources)
	if err != nil {
		return detail, &BackupError{BackupStageReadback, v.ID, err}
	}
	if err := checkBackupContents(dataRoot, before); err != nil {
		return detail, &BackupError{BackupStageReadback, v.ID, err}
	}
	latest, err := r.inventory(r.config.RuntimeDataDir, v.Sources)
	if err != nil {
		return detail, &BackupError{BackupStageReadback, v.ID, err}
	}
	if !sameBackupInventory(before, after) || !sameBackupInventory(before, latest) {
		return detail, &BackupError{BackupStageReadback, v.ID, ErrBackupConflict}
	}
	if err := r.inject(BackupStageSeal); err != nil {
		return detail, &BackupError{BackupStageSeal, v.ID, err}
	}
	state := "snapshot_verified"
	if v.Role == "apply" {
		state = "point_in_time_verified"
	}
	bundle := models.BackupBundle{Version: 1, ID: v.ID, Role: v.Role, PlanID: v.PlanID, ActionIDs: append([]string(nil), v.ActionIDs...), CreatedAt: created, Sources: append([]string(nil), v.Sources...), Files: before.Files, Directories: before.Directories, FileCount: before.FileCount, TotalSize: before.TotalSize, InventoryHash: before.InventoryHash}
	if err := r.writer.WriteJSON(filepath.Join(dir, "inventory.json"), bundle, 0600); err != nil {
		return detail, &BackupError{BackupStageSeal, v.ID, err}
	}
	registry := models.BackupRegistry{Version: 1, ID: v.ID, Role: v.Role, State: state, UpdatedAt: r.now()}
	if err := r.writer.WriteJSON(filepath.Join(dir, "registry.json"), registry, 0600); err != nil {
		return detail, &BackupError{BackupStageSeal, v.ID, err}
	}
	detail = models.BackupDetail{Bundle: bundle, Registry: registry}
	if _, err := r.loadLocked(v.ID); err != nil {
		return models.BackupDetail{}, &BackupError{BackupStageSeal, v.ID, err}
	}
	return detail, nil
}

func (r *BackupRepository) syncRoot() error { return syncTrashDirectory(r.config.Dir) }
func (r *BackupRepository) inject(stage BackupStage) error {
	if r.beforeStage != nil {
		return r.beforeStage(stage)
	}
	return nil
}

type backupInventory struct {
	Files                []models.BackupFile
	Directories          []string
	FileCount, TotalSize int64
	InventoryHash        string
}

type BackupSourceInventory struct {
	Files         []models.BackupFile
	Directories   []string
	FileCount     int64
	TotalSize     int64
	InventoryHash string
}

// InspectSources returns a stable, read-only snapshot of runtime sources.
func (r *BackupRepository) InspectSources(sources []string) (BackupSourceInventory, error) {
	if !validBackupSources(sources) {
		return BackupSourceInventory{}, ErrBackupBoundary
	}
	var result BackupSourceInventory
	err := r.withLock(func() error {
		inv, err := r.inventory(r.config.RuntimeDataDir, sources)
		if err != nil {
			return err
		}
		result = BackupSourceInventory{Files: inv.Files, Directories: inv.Directories, FileCount: inv.FileCount, TotalSize: inv.TotalSize, InventoryHash: inv.InventoryHash}
		return nil
	})
	return result, err
}

func ComputeBackupInventoryHash(files []models.BackupFile, directories []string) (string, error) {
	return backupManifestHash(files, directories)
}

func (r *BackupRepository) inventory(root string, sources []string) (backupInventory, error) {
	inv := backupInventory{Files: []models.BackupFile{}, Directories: []string{}}
	if root != r.config.RuntimeDataDir {
		if err := validateDirectory(root, r.uid); err != nil {
			return inv, fmt.Errorf("%w: snapshot root: %v", ErrBackupBoundary, err)
		}
	}
	if len(sources) == 0 {
		return inv, ErrBackupBoundary
	}
	for _, rel := range sources {
		if !validTrashRelative(rel) {
			return inv, ErrBackupBoundary
		}
	}
	for _, rel := range sources {
		if err := r.walkBackup(root, rel, &inv); err != nil {
			return inv, err
		}
	}
	sort.Slice(inv.Files, func(i, j int) bool { return inv.Files[i].Path < inv.Files[j].Path })
	sort.Strings(inv.Directories)
	var err error
	inv.InventoryHash, err = backupManifestHash(inv.Files, inv.Directories)
	if err != nil {
		return inv, err
	}
	return inv, nil
}

func backupManifestHash(files []models.BackupFile, dirs []string) (string, error) {
	if files == nil {
		files = []models.BackupFile{}
	}
	if dirs == nil {
		dirs = []string{}
	}
	encoded, err := json.Marshal(struct {
		Files       []models.BackupFile `json:"files"`
		Directories []string            `json:"directories"`
	}{files, dirs})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (r *BackupRepository) walkBackup(root, rel string, inv *backupInventory) error {
	path := filepath.Join(root, rel)
	if err := rejectSymlinkAncestors(path); err != nil {
		return fmt.Errorf("%w: source symlink or missing: %v", ErrBackupBoundary, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if root != r.config.RuntimeDataDir {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != r.uid {
			return ErrBackupBoundary
		}
		if info.IsDir() && info.Mode().Perm() != 0700 {
			return ErrBackupBoundary
		}
		if info.Mode().IsRegular() && info.Mode().Perm() != 0600 {
			return ErrBackupBoundary
		}
	}
	if info.IsDir() {
		inv.Directories = append(inv.Directories, filepath.ToSlash(rel))
		items, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := r.walkBackup(root, filepath.Join(rel, item.Name()), inv); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return ErrBackupBoundary
	}
	data, err := readBackupFile(path, info)
	if err != nil {
		return err
	}
	jsonStatus := ""
	if strings.EqualFold(filepath.Ext(path), ".json") {
		switch {
		case len(data) == 0:
			jsonStatus = "empty"
		case json.Valid(data):
			jsonStatus = "valid"
		default:
			jsonStatus = "invalid"
		}
	}
	sum := sha256.Sum256(data)
	inv.Files = append(inv.Files, models.BackupFile{Path: filepath.ToSlash(rel), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), JSONStatus: jsonStatus})
	inv.FileCount++
	inv.TotalSize += int64(len(data))
	return nil
}

func readBackupFile(path string, info os.FileInfo) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrBackupConflict
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, ErrBackupConflict
	}
	return data, nil
}

func sameBackupInventory(a, b backupInventory) bool {
	if a.FileCount != b.FileCount || a.TotalSize != b.TotalSize || a.InventoryHash != b.InventoryHash {
		return false
	}
	return true
}

func checkBackupContents(root string, inv backupInventory) error {
	allowed := map[string]bool{root: true}
	for _, dir := range inv.Directories {
		allowed[filepath.Join(root, dir)] = true
	}
	for _, file := range inv.Files {
		allowed[filepath.Join(root, file.Path)] = true
	}
	for path := range allowed {
		for parent := filepath.Dir(path); parent != root && containsPath(root, parent); parent = filepath.Dir(parent) {
			allowed[parent] = true
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !allowed[path] || entry.Type()&os.ModeSymlink != 0 {
			return ErrBackupCorrupt
		}
		return nil
	})
}

func (r *BackupRepository) copyInventory(inv backupInventory, destRoot string) error {
	return r.copyInventoryFrom(r.config.RuntimeDataDir, inv, destRoot)
}

func (r *BackupRepository) copyInventoryFrom(sourceRoot string, inv backupInventory, destRoot string) error {
	for _, rel := range inv.Directories {
		path := filepath.Join(destRoot, rel)
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		if err := os.Chmod(path, 0700); err != nil {
			return err
		}
	}
	for _, file := range inv.Files {
		if r.beforeCopyFile != nil {
			if err := r.beforeCopyFile(file.Path); err != nil {
				return err
			}
		}
		source := filepath.Join(sourceRoot, file.Path)
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		data, err := readBackupFile(source, info)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			return ErrBackupConflict
		}
		target := filepath.Join(destRoot, file.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		for parent := filepath.Dir(target); parent != destRoot; parent = filepath.Dir(parent) {
			if err := os.Chmod(parent, 0700); err != nil {
				return err
			}
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		n, writeErr := f.Write(data)
		if writeErr == nil && n != len(data) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		if err := os.Chmod(target, 0600); err != nil {
			return err
		}
	}
	dirs := map[string]bool{}
	for _, rel := range inv.Directories {
		dirs[filepath.Join(destRoot, rel)] = true
	}
	for _, file := range inv.Files {
		for parent := filepath.Dir(filepath.Join(destRoot, file.Path)); parent != destRoot; parent = filepath.Dir(parent) {
			dirs[parent] = true
		}
	}
	paths := make([]string, 0, len(dirs))
	for path := range dirs {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		if err := syncTrashDirectory(path); err != nil {
			return err
		}
	}
	return syncTrashDirectory(destRoot)
}

func (r *BackupRepository) Load(id string) (models.BackupDetail, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return models.BackupDetail{}, ErrMaintenanceID
	}
	var detail models.BackupDetail
	err := r.withLock(func() error { var err error; detail, err = r.loadLocked(id); return err })
	return detail, err
}

func (r *BackupRepository) loadLocked(id string) (models.BackupDetail, error) {
	var detail models.BackupDetail
	dir := filepath.Join(r.config.Dir, id)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return detail, ErrBackupMissing
	} else if err != nil {
		return detail, err
	}
	if err := validateDirectory(dir, r.uid); err != nil {
		return detail, ErrBackupBoundary
	}
	if err := readBackupJSON(filepath.Join(dir, "registry.json"), r.uid, &detail.Registry); err != nil {
		return detail, err
	}
	if detail.Registry.Version != 1 || detail.Registry.ID != id || (detail.Registry.Role != "rehearsal" && detail.Registry.Role != "apply") {
		return detail, ErrBackupCorrupt
	}
	if detail.Registry.State == "draft" {
		return detail, nil
	}
	if detail.Registry.State != "snapshot_verified" && detail.Registry.State != "point_in_time_verified" && detail.Registry.State != "rehearsal_verified" {
		return detail, ErrBackupCorrupt
	}
	if err := readBackupJSON(filepath.Join(dir, "inventory.json"), r.uid, &detail.Bundle); err != nil {
		return detail, err
	}
	b := detail.Bundle
	if b.Version != 1 || b.ID != id || b.Role != detail.Registry.Role || len(b.Sources) == 0 || b.FileCount != int64(len(b.Files)) || b.InventoryHash == "" {
		return detail, ErrBackupCorrupt
	}
	manifestHash, err := backupManifestHash(b.Files, b.Directories)
	if err != nil || manifestHash != b.InventoryHash {
		return detail, ErrBackupCorrupt
	}
	actual, err := r.inventory(filepath.Join(dir, "data"), b.Sources)
	if err != nil || actual.InventoryHash != b.InventoryHash || actual.FileCount != b.FileCount || actual.TotalSize != b.TotalSize {
		return detail, ErrBackupCorrupt
	}
	if err := checkBackupContents(filepath.Join(dir, "data"), actual); err != nil {
		return detail, ErrBackupCorrupt
	}
	if b.Role == "apply" && detail.Registry.State != "point_in_time_verified" {
		return detail, ErrBackupCorrupt
	}
	return detail, nil
}

func readBackupJSON(path string, uid uint32, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return ErrBackupCorrupt
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return ErrBackupBoundary
	}
	data, err := readBackupFile(path, info)
	if err != nil {
		return ErrBackupCorrupt
	}
	if len(data) > 4<<20 || json.Unmarshal(data, target) != nil {
		return ErrBackupCorrupt
	}
	return nil
}
