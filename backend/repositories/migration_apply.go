package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrMigrationWriteConflict = errors.New("migration path already exists or is unsafe")

// MigrationApplyRepository only creates new entries below its runtime root.
// It never edits or removes a legacy source.
type MigrationApplyRepository struct {
	Root   string
	Writer interface {
		WriteJSONNew(string, any, os.FileMode) error
	}
}

func NewMigrationApplyRepository(root string) *MigrationApplyRepository {
	return &MigrationApplyRepository{Root: root, Writer: NewAtomicJSONWriter()}
}

func (r *MigrationApplyRepository) target(rel string) (string, error) {
	if r == nil || r.Writer == nil || !filepath.IsAbs(r.Root) || rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, `\:`) || filepath.Clean(rel) != rel || (rel != "rooms" && !strings.HasPrefix(filepath.ToSlash(rel), "rooms/")) {
		return "", ErrMigrationWriteConflict
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." || part == ".." {
			return "", ErrMigrationWriteConflict
		}
	}
	root := filepath.Clean(r.Root)
	for p := root; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.Join(ErrMigrationWriteConflict, err)
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
			break
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.Join(ErrMigrationWriteConflict, err)
		}
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

func (r *MigrationApplyRepository) CreateDirectory(rel string) error {
	target, err := r.target(rel)
	if err != nil {
		return err
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return errors.Join(ErrMigrationWriteConflict, err)
	}
	return syncTrashDirectory(filepath.Dir(target))
}

func (r *MigrationApplyRepository) CreateJSON(rel string, value any) error {
	if rel == "rooms" {
		return ErrMigrationWriteConflict
	}
	target, err := r.target(rel)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Dir(target)); err != nil || !info.IsDir() {
		return errors.Join(ErrMigrationWriteConflict, err)
	}
	if _, err := os.Lstat(target); err == nil {
		return ErrMigrationWriteConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := r.Writer.WriteJSONNew(target, value, 0600); err != nil {
		return err
	}
	return nil
}
