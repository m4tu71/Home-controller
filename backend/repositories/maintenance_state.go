package repositories

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

var (
	ErrMaintenanceState         = errors.New("invalid maintenance state")
	ErrMaintenanceID            = errors.New("invalid maintenance ID")
	ErrMaintenanceMissing       = errors.New("maintenance record missing")
	ErrMaintenanceDuplicate     = errors.New("maintenance record already exists")
	ErrMaintenanceCorrupt       = errors.New("corrupt maintenance record")
	ErrMaintenanceConflict      = errors.New("maintenance revision conflict")
	ErrMaintenancePlanClaimed   = errors.New("maintenance plan already claimed")
	ErrMaintenanceTargetClaimed = errors.New("maintenance target already claimed")
	maintenanceIDPattern        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
)

type MaintenanceStateConfig struct {
	Dir            string
	RuntimeDataDir string
	RepositoryDir  string
	WebRootDir     string
	BackupDir      string
	TrashDir       string
}

type MaintenanceState struct {
	config MaintenanceStateConfig
	uid    uint32
	writer JSONWriter
}

func NewMaintenanceStateFromEnv() (*MaintenanceState, error) {
	return NewMaintenanceStateFromEnvWithRuntimeDataDir(filepath.Dir(defaultRoomsDir))
}

// NewMaintenanceStateFromEnvWithRuntimeDataDir keeps the legacy environment
// contract while allowing callers that already selected a runtime data root
// to use that root for validation and path resolution.
func NewMaintenanceStateFromEnvWithRuntimeDataDir(runtimeDataDir string) (*MaintenanceState, error) {
	return NewMaintenanceState(MaintenanceStateConfig{
		Dir:            os.Getenv("WEB_CONTROLLER_MAINTENANCE_STATE_DIR"),
		RuntimeDataDir: runtimeDataDir,
		RepositoryDir:  filepath.Dir(filepath.Dir(defaultRoomsDir)),
		WebRootDir:     "/var/www/web-controller",
		BackupDir:      os.Getenv("WEB_CONTROLLER_BACKUP_DIR"),
		TrashDir:       os.Getenv("WEB_CONTROLLER_TRASH_DIR"),
	})
}

func NewMaintenanceState(config MaintenanceStateConfig) (*MaintenanceState, error) {
	s := &MaintenanceState{config: config, uid: uint32(os.Geteuid()), writer: NewAtomicJSONWriter()}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *MaintenanceState) Plans() *MaintenancePlanRepository { return &MaintenancePlanRepository{s} }
func (s *MaintenanceState) Operations() *MaintenanceOperationRepository {
	return &MaintenanceOperationRepository{s}
}

func (s *MaintenanceState) validate() error {
	if s == nil || s.config.Dir == "" || !filepath.IsAbs(s.config.Dir) {
		return fmt.Errorf("%w: absolute state directory required", ErrMaintenanceState)
	}
	root := filepath.Clean(s.config.Dir)
	for _, forbidden := range []string{s.config.RuntimeDataDir, s.config.RepositoryDir, s.config.WebRootDir, s.config.BackupDir, s.config.TrashDir} {
		if forbidden == "" {
			continue
		}
		if !filepath.IsAbs(forbidden) {
			return fmt.Errorf("%w: protected path is relative", ErrMaintenanceState)
		}
		p := filepath.Clean(forbidden)
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if containsPath(root, p) || containsPath(p, root) {
			return fmt.Errorf("%w: state overlaps protected path %s", ErrMaintenanceState, p)
		}
	}
	for _, dir := range []string{root, filepath.Join(root, "plans"), filepath.Join(root, "operations")} {
		if err := validateDirectory(dir, s.uid); err != nil {
			return err
		}
	}
	return nil
}

func containsPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !filepath.IsAbs(rel) && len(rel) >= 3 && rel[:3] != "../"))
}

func validateDirectory(path string, uid uint32) error {
	if err := rejectSymlinkAncestors(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrMaintenanceState, path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || stat.Uid != uid || info.Mode().Perm() != 0700 {
		return fmt.Errorf("%w: directory %s must be owned by uid %d with mode 0700", ErrMaintenanceState, path, uid)
	}
	return nil
}

func rejectSymlinkAncestors(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrMaintenanceState, p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %s", ErrMaintenanceState, p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

func (s *MaintenanceState) withLock(fn func() error) error {
	if err := s.validate(); err != nil {
		return err
	}
	f, err := os.Open(s.config.Dir)
	if err != nil {
		return fmt.Errorf("%w: lock root: %v", ErrMaintenanceState, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("%w: lock: %v", ErrMaintenanceState, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if err := s.validate(); err != nil {
		return err
	}
	return fn()
}

func maintenancePath(s *MaintenanceState, group, id string) (string, error) {
	if !maintenanceIDPattern.MatchString(id) {
		return "", ErrMaintenanceID
	}
	return filepath.Join(s.config.Dir, group, id+".json"), nil
}

func (s *MaintenanceState) readFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMaintenanceMissing
	}
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != s.uid || info.Mode().Perm() != 0600 {
		return nil, fmt.Errorf("%w: file owner/type/mode %s", ErrMaintenanceState, path)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: secure open: %v", ErrMaintenanceState, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%w: file changed during open", ErrMaintenanceState)
	}
	data, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, ErrMaintenanceCorrupt
	}
	return data, nil
}
