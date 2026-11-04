package repositories

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DataHealthNode struct {
	Relative  string
	Absolute  string
	Directory bool
	Symlink   bool
	Names     []string
	Bytes     []byte
	Hash      string
}

type DataHealthRepository struct{ Root string }

func NewDataHealthRepository(root string) *DataHealthRepository {
	return &DataHealthRepository{Root: root}
}

// Scan never follows links. A failed read is returned as an error, never
// silently omitted from the inventory.
func (r *DataHealthRepository) Scan() ([]DataHealthNode, error) {
	if r.Root == "" || !filepath.IsAbs(r.Root) || filepath.Clean(r.Root) != r.Root {
		return nil, fmt.Errorf("data root must be a clean absolute path")
	}
	info, err := os.Lstat(r.Root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("data root must be a real directory")
	}
	resolved, err := filepath.EvalSymlinks(r.Root)
	if err != nil || resolved != r.Root {
		return nil, fmt.Errorf("data root must not contain symbolic links")
	}
	var nodes []DataHealthNode
	err = filepath.WalkDir(r.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(r.Root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("path escapes data root: %q", rel)
		}
		n := DataHealthNode{Relative: filepath.ToSlash(rel), Absolute: path, Directory: entry.IsDir(), Symlink: entry.Type()&os.ModeSymlink != 0}
		if n.Directory {
			children, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			for _, child := range children {
				n.Names = append(n.Names, child.Name())
			}
			sort.Strings(n.Names)
			n.Hash = dataHealthHash([]byte(strings.Join(n.Names, "\x00")))
		} else if n.Symlink {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			n.Hash = dataHealthHash([]byte(target))
		} else {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported file type: %s", path)
			}
			// Lstat immediately before read makes a link swap detectable; a
			// second check catches a swap during the read.
			before, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !before.Mode().IsRegular() {
				return fmt.Errorf("file changed during scan: %s", path)
			}
			n.Bytes, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			after, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !os.SameFile(before, after) || !after.Mode().IsRegular() {
				return errors.New("file changed during scan: " + path)
			}
			n.Hash = dataHealthHash(n.Bytes)
		}
		nodes = append(nodes, n)
		return nil
	})
	return nodes, err
}

func dataHealthHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
