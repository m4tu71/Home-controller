package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/models"
)

func backupFixture(t *testing.T) (*BackupRepository, BackupConfig) {
	t.Helper()
	base := t.TempDir()
	c := BackupConfig{Dir: filepath.Join(base, "backup"), RuntimeDataDir: filepath.Join(base, "data"), RepositoryDir: filepath.Join(base, "repo"), WebRootDir: filepath.Join(base, "web"), StateDir: filepath.Join(base, "state"), TrashDir: filepath.Join(base, "trash")}
	for _, path := range []string{c.Dir, c.RuntimeDataDir, c.RepositoryDir, c.WebRootDir, c.StateDir, c.TrashDir} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(c.RuntimeDataDir, "rooms", "room_1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.RuntimeDataDir, "rooms", "room_1", "room.json"), []byte(`{"id":"room_1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := NewBackupRepository(c)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}

func backupRequest(role string) BackupCreate {
	return BackupCreate{ID: "backup_1", Role: role, PlanID: "plan_1", ActionIDs: []string{"action_1"}, Sources: []string{"rooms/room_1"}}
}

func TestBackupCreateRolesAndReadback(t *testing.T) {
	for _, tc := range []struct{ role, state string }{{"rehearsal", "snapshot_verified"}, {"apply", "point_in_time_verified"}} {
		t.Run(tc.role, func(t *testing.T) {
			r, c := backupFixture(t)
			detail, err := r.Create(backupRequest(tc.role))
			if err != nil || detail.Registry.State != tc.state || detail.Bundle.FileCount != 1 || detail.Bundle.TotalSize == 0 || len(detail.Bundle.Files) != 1 || !strings.HasPrefix(detail.Bundle.InventoryHash, "sha256:") {
				t.Fatalf("detail=%+v err=%v", detail, err)
			}
			t.Logf("fixture canonical_root=%s backup=%s role=%s count=%d size=%d inventory_hash=%s", c.Dir, detail.Bundle.ID, tc.role, detail.Bundle.FileCount, detail.Bundle.TotalSize, detail.Bundle.InventoryHash)
			loaded, err := r.Load("backup_1")
			if err != nil || loaded.Bundle.InventoryHash != detail.Bundle.InventoryHash {
				t.Fatalf("load=%+v err=%v", loaded, err)
			}
			if _, err := r.Create(backupRequest(tc.role)); !errors.Is(err, ErrBackupConflict) {
				t.Fatalf("duplicate=%v", err)
			}
			file := filepath.Join(c.Dir, "backup_1", "data", "rooms", "room_1", "room.json")
			if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("file mode=%v %v", info, err)
			}
			if info, err := os.Stat(filepath.Dir(file)); err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("dir mode=%v %v", info, err)
			}
			if err := os.WriteFile(file, []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Load("backup_1"); !errors.Is(err, ErrBackupCorrupt) {
				t.Fatalf("tamper=%v", err)
			}
		})
	}
}

func TestBackupBoundaryAndCapacity(t *testing.T) {
	r, c := backupFixture(t)
	missingState := c
	missingState.StateDir = ""
	if _, err := NewBackupRepository(missingState); !errors.Is(err, ErrBackupBoundary) {
		t.Fatalf("missing state root: %v", err)
	}
	for _, forbidden := range []string{c.RuntimeDataDir, c.RepositoryDir, c.WebRootDir, c.StateDir, c.TrashDir} {
		inside := filepath.Join(forbidden, "nested")
		if err := os.Mkdir(inside, 0700); err != nil {
			t.Fatal(err)
		}
		candidate := c
		candidate.Dir = inside
		if _, err := NewBackupRepository(candidate); !errors.Is(err, ErrBackupBoundary) {
			t.Fatalf("inside %s: %v", forbidden, err)
		}
	}
	wrong := c
	if err := os.Chmod(c.Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBackupRepository(wrong); !errors.Is(err, ErrBackupBoundary) {
		t.Fatalf("wrong mode: %v", err)
	}
	if err := os.Chmod(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	ownerMismatch := *r
	ownerMismatch.uid++
	if err := ownerMismatch.validate(); !errors.Is(err, ErrBackupBoundary) {
		t.Fatalf("wrong owner: %v", err)
	}
	link := filepath.Join(filepath.Dir(c.Dir), "backup-link")
	if err := os.Symlink(c.Dir, link); err != nil {
		t.Fatal(err)
	}
	linked := c
	linked.Dir = link
	if _, err := NewBackupRepository(linked); !errors.Is(err, ErrBackupBoundary) {
		t.Fatalf("symlink root: %v", err)
	}
	r.available = func(string) (uint64, error) { return 0, nil }
	if _, err := r.Create(backupRequest("rehearsal")); !errors.Is(err, ErrBackupCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "backup_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bundle created despite capacity failure: %v", err)
	}
}

func TestBackupSnapshotModeMismatch(t *testing.T) {
	r, c := backupFixture(t)
	if _, err := r.Create(backupRequest("rehearsal")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, "backup_1", "data", "rooms", "room_1", "room.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load("backup_1"); !errors.Is(err, ErrBackupCorrupt) {
		t.Fatalf("snapshot file mode: %v", err)
	}
}

func TestBackupCreateRejectsPlanInventoryMismatchBeforeCreatingBundle(t *testing.T) {
	r, c := backupFixture(t)
	request := backupRequest("rehearsal")
	request.ExpectedInventoryHash = "sha256:" + strings.Repeat("0", 64)
	if _, err := r.Create(request); !errors.Is(err, ErrBackupConflict) {
		t.Fatalf("expected stale-plan conflict, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(c.Dir, request.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale plan created a backup bundle: %v", err)
	}
}

func TestBackupRejectsUnexpectedSnapshotFile(t *testing.T) {
	r, c := backupFixture(t)
	if _, err := r.Create(backupRequest("rehearsal")); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(c.Dir, "backup_1", "data", "unexpected.json")
	if err := os.WriteFile(extra, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load("backup_1"); !errors.Is(err, ErrBackupCorrupt) {
		t.Fatalf("extra snapshot file: %v", err)
	}
}

func TestBackupRejectsSymlink(t *testing.T) {
	r, c := backupFixture(t)
	link := filepath.Join(c.RuntimeDataDir, "rooms", "linked")
	if err := os.Symlink(filepath.Join(c.RuntimeDataDir, "rooms", "room_1"), link); err != nil {
		t.Fatal(err)
	}
	v := backupRequest("rehearsal")
	v.Sources = []string{"rooms/linked"}
	if _, err := r.Create(v); !errors.Is(err, ErrBackupBoundary) {
		t.Fatalf("symlink: %v", err)
	}
}

func TestBackupPreservesInvalidAndEmptyJSON(t *testing.T) {
	for _, role := range []string{"rehearsal", "apply"} {
		t.Run(role, func(t *testing.T) {
			r, c := backupFixture(t)
			invalidPath := filepath.Join(c.RuntimeDataDir, "rooms", "room_1", "room.json")
			legacyDir := filepath.Join(c.RuntimeDataDir, "ir-signals")
			if err := os.Mkdir(legacyDir, 0700); err != nil {
				t.Fatal(err)
			}
			emptyPath := filepath.Join(legacyDir, "legacy.json")
			if err := os.WriteFile(invalidPath, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(emptyPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			request := backupRequest(role)
			request.Sources = []string{"rooms/room_1/room.json", "ir-signals/legacy.json"}
			detail, err := r.Create(request)
			if err != nil || detail.Bundle.FileCount != 2 || detail.Bundle.TotalSize != 1 {
				t.Fatalf("detail=%+v err=%v", detail, err)
			}
			statuses := map[string]string{}
			for _, file := range detail.Bundle.Files {
				statuses[file.Path] = file.JSONStatus
			}
			if statuses["rooms/room_1/room.json"] != "invalid" || statuses["ir-signals/legacy.json"] != "empty" {
				t.Fatalf("statuses=%v", statuses)
			}
			for _, rel := range request.Sources {
				source, err := os.ReadFile(filepath.Join(c.RuntimeDataDir, rel))
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := os.ReadFile(filepath.Join(c.Dir, "backup_1", "data", rel))
				if err != nil {
					t.Fatal(err)
				}
				if string(source) != string(snapshot) {
					t.Fatalf("bytes differ for %s", rel)
				}
			}
			if _, err := r.Load("backup_1"); err != nil {
				t.Fatalf("sealed readback: %v", err)
			}
		})
	}
}

func TestBackupDraftOnFailures(t *testing.T) {
	for _, stage := range []BackupStage{BackupStageDraft, BackupStageCopy, BackupStageReadback, BackupStageSeal} {
		t.Run(string(stage), func(t *testing.T) {
			r, _ := backupFixture(t)
			r.beforeStage = func(current BackupStage) error {
				if current == stage {
					return errors.New("injected " + string(stage))
				}
				return nil
			}
			_, err := r.Create(backupRequest("rehearsal"))
			var backupErr *BackupError
			if !errors.As(err, &backupErr) || backupErr.Stage != stage {
				t.Fatalf("error=%v", err)
			}
			detail, err := r.Load("backup_1")
			if err != nil || detail.Registry.State != "draft" {
				t.Fatalf("draft=%+v err=%v", detail, err)
			}
		})
	}
}

func TestBackupReadbackMismatch(t *testing.T) {
	r, c := backupFixture(t)
	r.beforeStage = func(stage BackupStage) error {
		if stage == BackupStageReadback {
			return os.WriteFile(filepath.Join(c.Dir, "backup_1", "data", "rooms", "room_1", "room.json"), []byte(`{"changed":true}`), 0600)
		}
		return nil
	}
	if _, err := r.Create(backupRequest("rehearsal")); !errors.Is(err, ErrBackupConflict) {
		t.Fatalf("readback mismatch: %v", err)
	}
	if _, err := r.Load("backup_1"); err != nil {
		t.Fatalf("draft readback: %v", err)
	}
}

func TestBackupSourceChangeDuringCopy(t *testing.T) {
	r, c := backupFixture(t)
	r.beforeStage = func(stage BackupStage) error {
		if stage == BackupStageReadback {
			return os.WriteFile(filepath.Join(c.RuntimeDataDir, "rooms", "room_1", "room.json"), []byte(`{"id":"changed"}`), 0600)
		}
		return nil
	}
	if _, err := r.Create(backupRequest("rehearsal")); !errors.Is(err, ErrBackupConflict) {
		t.Fatalf("source change: %v", err)
	}
}

func TestBackupCopyFailureLeavesDraft(t *testing.T) {
	r, _ := backupFixture(t)
	r.beforeCopyFile = func(string) error { return errors.New("injected copy failure") }
	_, err := r.Create(backupRequest("rehearsal"))
	var stageErr *BackupError
	if !errors.As(err, &stageErr) || stageErr.Stage != BackupStageCopy {
		t.Fatalf("copy error=%v", err)
	}
	detail, err := r.Load("backup_1")
	if err != nil || detail.Registry.State != "draft" {
		t.Fatalf("draft=%+v err=%v", detail, err)
	}
}

func TestBackupInventoryManifestHash(t *testing.T) {
	r, c := backupFixture(t)
	if _, err := r.Create(backupRequest("rehearsal")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, "backup_1", "inventory.json")
	var bundle models.BackupBundle
	if err := readBackupJSON(path, r.uid, &bundle); err != nil {
		t.Fatal(err)
	}
	bundle.Files[0].SHA256 = strings.Repeat("0", 64)
	if err := r.writer.WriteJSON(path, bundle, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load("backup_1"); !errors.Is(err, ErrBackupCorrupt) {
		t.Fatalf("manifest tamper: %v", err)
	}
}
