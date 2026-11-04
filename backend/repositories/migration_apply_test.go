package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationApplyRepositoryCreatesPlannedRoomsRootOnlyAsDirectory(t *testing.T) {
	root := t.TempDir()
	repo := NewMigrationApplyRepository(root)
	if err := repo.CreateJSON("rooms", map[string]string{"unsafe": "file"}); !errors.Is(err, ErrMigrationWriteConflict) {
		t.Fatalf("rooms JSON err=%v", err)
	}
	if err := repo.CreateDirectory("rooms"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, "rooms"))
	if err != nil || !info.IsDir() {
		t.Fatalf("rooms directory info=%+v err=%v", info, err)
	}
	if err := repo.CreateDirectory("rooms"); !errors.Is(err, ErrMigrationWriteConflict) {
		t.Fatalf("duplicate err=%v", err)
	}
	for _, unsafe := range []string{"../outside", "room", "rooms/../outside", "rooms\\outside"} {
		if err := repo.CreateDirectory(unsafe); !errors.Is(err, ErrMigrationWriteConflict) {
			t.Fatalf("unsafe %q err=%v", unsafe, err)
		}
	}
}
