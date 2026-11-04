package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/models"
)

func TestRoomRepositoryAtomicSaveReadback(t *testing.T) {
	base := t.TempDir()
	repo := NewRoomRepositoryWithBaseDir(base)
	old := models.Room{ID: "bedroom", Name: "Bedroom", SortOrder: 1}
	next := models.Room{ID: "bedroom", Name: "Updated", SortOrder: 2}
	for _, room := range []models.Room{old, next} {
		if err := repo.Save(room); err != nil {
			t.Fatal(err)
		}
		fresh := NewRoomRepositoryWithBaseDir(base)
		got, err := fresh.Load(room.ID)
		if err != nil || !reflect.DeepEqual(got, room) {
			t.Fatalf("fresh repository readback: got %+v, err %v; want %+v", got, err, room)
		}
	}
	entries, err := os.ReadDir(filepath.Join(base, old.ID))
	if err != nil || len(entries) != 1 || entries[0].Name() != "room.json" {
		t.Fatalf("unexpected room directory: %v, %v", entries, err)
	}
}

func TestRoomRepositoryAtomicFailureBoundary(t *testing.T) {
	stages := []AtomicWriteStage{AtomicWriteCreateTemp, AtomicWriteWrite, AtomicWriteChmod, AtomicWriteFileSync, AtomicWriteFileClose, AtomicWriteRename, AtomicWriteParentSync}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			base := t.TempDir()
			repo := NewRoomRepositoryWithBaseDir(base)
			old := models.Room{ID: "bedroom", Name: "Old"}
			next := models.Room{ID: "bedroom", Name: "New"}
			if err := repo.Save(old); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected room write failure")
			repo.writer = &AtomicJSONWriter{beforeStage: func(current AtomicWriteStage) error {
				if current == stage {
					return injected
				}
				return nil
			}}
			err := repo.Save(next)
			var writeErr *AtomicWriteError
			if !errors.As(err, &writeErr) || !errors.Is(err, injected) || writeErr.Stage != stage || writeErr.TargetReplaced != (stage == AtomicWriteParentSync) {
				t.Fatalf("unexpected failure boundary: %v", err)
			}
			fresh := NewRoomRepositoryWithBaseDir(base)
			got, err := fresh.Load(old.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := old
			if stage == AtomicWriteParentSync {
				want = next
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("readback after %s: got %+v; want %+v", stage, got, want)
			}
			entries, err := os.ReadDir(filepath.Join(base, old.ID))
			if err != nil || len(entries) != 1 || entries[0].Name() != "room.json" {
				t.Fatalf("temporary file remains: %v, %v", entries, err)
			}
		})
	}
}

func TestRoomRepositoryFailedCreateDoesNotExposePartialJSON(t *testing.T) {
	base := t.TempDir()
	repo := NewRoomRepositoryWithBaseDir(base)
	repo.writer = &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		if stage == AtomicWriteWrite {
			return errors.New("injected")
		}
		return nil
	}}
	if err := repo.Save(models.Room{ID: "bedroom", Name: "Bedroom"}); err == nil {
		t.Fatal("expected write failure")
	}
	fresh := NewRoomRepositoryWithBaseDir(base)
	if _, err := fresh.Load("bedroom"); !os.IsNotExist(err) {
		t.Fatalf("partial room became readable: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(base, "bedroom"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected only empty parent directory: %v, %v", entries, err)
	}
}

func TestRoomRepositoryCorruptJSONReadbackFails(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "bedroom", "room.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRoomRepositoryWithBaseDir(base).Load("bedroom"); err == nil || !strings.Contains(err.Error(), "unexpected end") {
		t.Fatalf("expected corrupt JSON error, got %v", err)
	}
}
