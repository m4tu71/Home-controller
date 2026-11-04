package repositories

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"backend/models"
)

func trashInventory(path string) (models.TrashInventory, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return models.TrashInventory{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return models.TrashInventory{}, errors.New("symlink in Trash inventory")
	}
	if info.Mode().IsRegular() {
		digest, err := trashFileHash(path, info)
		if err != nil {
			return models.TrashInventory{}, err
		}
		return models.TrashInventory{Kind: "file", FileCount: 1, TotalSize: info.Size(), SHA256: digest}, nil
	}
	if !info.IsDir() {
		return models.TrashInventory{}, errors.New("unsupported Trash object")
	}
	h := sha256.New()
	inventory := models.TrashInventory{Kind: "directory"}
	if err := hashTrashDirectory(h, path, "", &inventory); err != nil {
		return models.TrashInventory{}, err
	}
	inventory.SHA256 = hex.EncodeToString(h.Sum(nil))
	return inventory, nil
}

func trashFileHash(path string, info os.FileInfo) (string, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("file changed during Trash open")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", errors.New("file changed during Trash read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashTrashDirectory(h hash.Hash, path, relative string, inventory *models.TrashInventory) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in Trash directory")
		}
		rel := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		if info.IsDir() {
			if _, err := fmt.Fprintf(h, "d:%s\n", rel); err != nil {
				return err
			}
			if err := hashTrashDirectory(h, child, rel, inventory); err != nil {
				return err
			}
		} else if info.Mode().IsRegular() {
			digest, err := trashFileHash(child, info)
			if err != nil {
				return err
			}
			inventory.FileCount++
			inventory.TotalSize += info.Size()
			if _, err := fmt.Fprintf(h, "f:%s:%d:%s\n", rel, info.Size(), digest); err != nil {
				return err
			}
		} else {
			return errors.New("unsupported object in Trash directory")
		}
	}
	return nil
}
