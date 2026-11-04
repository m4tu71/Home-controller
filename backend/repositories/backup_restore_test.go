package repositories

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sealedRehearsalFixture(t *testing.T) (*BackupRepository, BackupConfig) {
	t.Helper()
	r, c := backupFixture(t)
	if _, err := r.Create(backupRequest("rehearsal")); err != nil {
		t.Fatal(err)
	}
	return r, c
}

func TestBackupRestoreDryRunAndRehearsal(t *testing.T) {
	r, c := sealedRehearsalFixture(t)
	preview, err := r.RestoreDryRun("backup_1")
	if err != nil || !preview.CanRehearse || preview.TargetExists || preview.Target != "isolated-rehearsal/backup_1" || preview.SourceInventoryHash == "" || preview.RequiredBytes == 0 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if _, err := os.Lstat(filepath.Join(c.Dir, ".restore-rehearsal")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote target: %v", err)
	}
	result, err := r.RestoreRehearsal("backup_1")
	if err != nil || result.PreviousState != "snapshot_verified" || result.State != "rehearsal_verified" || result.InventoryHash != preview.SourceInventoryHash || result.FileCount != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	t.Logf("fixture restore target=%s hash=%s files=%d bytes=%d state=%s", filepath.Join(c.Dir, ".restore-rehearsal", "backup_1"), result.InventoryHash, result.FileCount, result.TotalSize, result.State)
	got, err := os.ReadFile(filepath.Join(c.Dir, ".restore-rehearsal", "backup_1", "rooms", "room_1", "room.json"))
	if err != nil || string(got) != `{"id":"room_1"}` {
		t.Fatalf("isolated result=%q err=%v", got, err)
	}
	loaded, err := r.Load("backup_1")
	if err != nil || loaded.Registry.State != "rehearsal_verified" || loaded.Bundle.InventoryHash != result.InventoryHash {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if _, err := r.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupRestoreConflict) {
		t.Fatalf("double rehearsal=%v", err)
	}
}

func TestBackupRestoreRejectsUnverifiedAndApplyRole(t *testing.T) {
	r, _ := backupFixture(t)
	r.beforeStage = func(stage BackupStage) error {
		if stage == BackupStageCopy {
			return errors.New("copy stop")
		}
		return nil
	}
	if _, err := r.Create(backupRequest("rehearsal")); err == nil {
		t.Fatal("expected interrupted draft")
	}
	p, err := r.RestoreDryRun("backup_1")
	if err != nil || p.CanRehearse || p.Reason != "backup_not_rehearsal_candidate" {
		t.Fatalf("draft preview=%+v err=%v", p, err)
	}
	if _, err := r.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupRestoreConflict) {
		t.Fatalf("draft rehearsal=%v", err)
	}
	r2, c := backupFixture(t)
	if _, err := r2.Create(backupRequest("apply")); err != nil {
		t.Fatal(err)
	}
	p, err = r2.RestoreDryRun("backup_1")
	if err != nil || p.CanRehearse || p.Reason != "wrong_backup_role" {
		t.Fatalf("apply preview=%+v err=%v", p, err)
	}
	if _, err := r2.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupRestoreConflict) {
		t.Fatalf("apply rehearsal=%v", err)
	}
	if _, err := r2.Load("missing"); !errors.Is(err, ErrBackupMissing) {
		t.Fatalf("missing fixture root=%s: %v", c.Dir, err)
	}
}

func TestBackupRestoreDryRunCapacityAndTargetCollision(t *testing.T) {
	r, c := sealedRehearsalFixture(t)
	r.available = func(string) (uint64, error) { return 0, nil }
	p, err := r.RestoreDryRun("backup_1")
	if err != nil || p.CanRehearse || p.Reason != "insufficient_capacity" {
		t.Fatalf("capacity=%+v err=%v", p, err)
	}
	r.available = func(string) (uint64, error) { return 1 << 30, nil }
	target := filepath.Join(c.Dir, ".restore-rehearsal", "backup_1")
	if err := os.MkdirAll(filepath.Join(target, "rooms", "room_1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "rooms", "room_1", "room.json"), []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = r.RestoreDryRun("backup_1")
	if err != nil || p.CanRehearse || !p.TargetExists || p.Reason != "target_collision" || p.TargetInventoryHash == "" || p.TargetInventoryHash == p.SourceInventoryHash {
		t.Fatalf("collision=%+v err=%v", p, err)
	}
	if _, err := r.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupRestoreConflict) {
		t.Fatalf("target overwrite allowed: %v", err)
	}
}

func TestBackupRestoreSymlinkAndMissingBundle(t *testing.T) {
	r, c := sealedRehearsalFixture(t)
	parent := filepath.Join(c.Dir, ".restore-rehearsal")
	if err := os.Symlink(c.RuntimeDataDir, parent); err != nil {
		t.Fatal(err)
	}
	p, err := r.RestoreDryRun("backup_1")
	if err != nil || p.CanRehearse || p.Reason != "unsafe_target" {
		t.Fatalf("symlink=%+v err=%v", p, err)
	}
	if _, err := r.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupRestoreConflict) {
		t.Fatalf("symlink rehearsal=%v", err)
	}
	if _, err := r.RestoreDryRun("absent"); !errors.Is(err, ErrBackupMissing) {
		t.Fatalf("missing=%v", err)
	}
}

func TestBackupRestoreInterruptionAndReadbackMismatch(t *testing.T) {
	for _, stage := range []BackupRestoreStage{BackupRestoreStagePrepare, BackupRestoreStageCopy, BackupRestoreStageReadback, BackupRestoreStageRepositoryReadback, BackupRestoreStagePromote} {
		t.Run(string(stage), func(t *testing.T) {
			r, c := sealedRehearsalFixture(t)
			r.beforeRestoreStage = func(current BackupRestoreStage) error {
				if current == stage {
					if stage == BackupRestoreStageReadback {
						return os.WriteFile(filepath.Join(c.Dir, ".restore-rehearsal", "backup_1", "rooms", "room_1", "room.json"), []byte("changed"), 0600)
					}
					if stage == BackupRestoreStageRepositoryReadback {
						return os.WriteFile(filepath.Join(c.Dir, ".restore-rehearsal", "backup_1", "rooms", "room_1", "room.json"), []byte("{"), 0600)
					}
					return errors.New("injected " + string(stage))
				}
				return nil
			}
			_, err := r.RestoreRehearsal("backup_1")
			var stageErr *BackupRestoreError
			if !errors.As(err, &stageErr) || stageErr.Stage != stage {
				t.Fatalf("error=%v", err)
			}
			bundle, loadErr := r.Load("backup_1")
			if loadErr != nil || bundle.Registry.State != "snapshot_verified" {
				t.Fatalf("source bundle changed: %+v err=%v", bundle, loadErr)
			}
			if stage == BackupRestoreStagePrepare {
				if _, statErr := os.Lstat(filepath.Join(c.Dir, ".restore-rehearsal")); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("prepare stage wrote target: %v", statErr)
				}
			}
		})
	}
}

type backupRestoreFailWriter struct {
	JSONWriter
	failPath string
}

func (w backupRestoreFailWriter) WriteJSON(path string, value any, mode os.FileMode) error {
	if path == w.failPath {
		return errors.New("injected registry state failure")
	}
	return w.JSONWriter.WriteJSON(path, value, mode)
}

func TestBackupRestorePromotionFailureAndMalformedJSONPreservation(t *testing.T) {
	r, c := backupFixture(t)
	if err := os.WriteFile(filepath.Join(c.RuntimeDataDir, "rooms", "room_1", "room.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(c.RuntimeDataDir, "ir-signals"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.RuntimeDataDir, "ir-signals", "empty.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	v := backupRequest("rehearsal")
	v.Sources = []string{"rooms/room_1/room.json", "ir-signals/empty.json"}
	created, err := r.Create(v)
	if err != nil {
		t.Fatal(err)
	}
	r.writer = backupRestoreFailWriter{JSONWriter: r.writer, failPath: filepath.Join(c.Dir, "backup_1", "registry.json")}
	if _, err := r.RestoreRehearsal("backup_1"); err == nil {
		t.Fatal("expected promotion failure")
	}
	r.writer = NewAtomicJSONWriter()
	loaded, err := r.Load("backup_1")
	if err != nil || loaded.Registry.State != "snapshot_verified" {
		t.Fatalf("state=%+v err=%v", loaded, err)
	}
	for _, file := range created.Bundle.Files {
		if file.JSONStatus != "invalid" && file.JSONStatus != "empty" {
			t.Fatalf("unexpected JSON state %s", file.JSONStatus)
		}
		source, err := os.ReadFile(filepath.Join(c.RuntimeDataDir, file.Path))
		if err != nil {
			t.Fatal(err)
		}
		rehearsal, err := os.ReadFile(filepath.Join(c.Dir, ".restore-rehearsal", "backup_1", filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(source) != string(rehearsal) {
			t.Fatalf("invalid/empty JSON bytes changed for %s", file.Path)
		}
	}
}

func TestBackupRestoreRegistryStateIsIndependent(t *testing.T) {
	r, c := sealedRehearsalFixture(t)
	dataPath := filepath.Join(c.Dir, "backup_1", "data", "rooms", "room_1", "room.json")
	before, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RestoreRehearsal("backup_1"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(dataPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("sealed snapshot changed")
	}
	metadata, err := os.ReadFile(filepath.Join(c.Dir, "backup_1", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "rehearsal_verified") {
		t.Fatal("mutable registry status leaked into sealed inventory")
	}
}

func TestBackupRestoreRepositoryReadbackForSupportedJSON(t *testing.T) {
	r, c := backupFixture(t)
	files := map[string]string{
		"rooms/room_1/devices/device_1/device.json":                     `{"id":"device_1","room_id":"room_1","capabilities":["ir"]}`,
		"rooms/room_1/devices/device_1/adapters/ir/ir_config.json":      `{"ir_remote_type":"raw","carrier_khz":38}`,
		"rooms/room_1/devices/device_1/adapters/ir/state_schema.json":   `{}`,
		"rooms/room_1/devices/device_1/adapters/ir/commands/power.json": `{"id":"power","name":"Power","format":"raw"}`,
		"rooms/room_1/devices/device_1/adapters/ir/states/cool.json":    `{"id":"cool","name":"Cool","state":{},"format":"raw"}`,
		"ir-signals/test.json": `{"name":"test","room":"room_1","device":"device_1","format":"raw","carrier_khz":38}`,
	}
	for relativePath, content := range files {
		path := filepath.Join(c.RuntimeDataDir, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(c.RuntimeDataDir, "ir-signals"), 0700); err != nil {
		t.Fatal(err)
	}
	request := backupRequest("rehearsal")
	request.Sources = []string{"rooms/room_1", "ir-signals/test.json"}
	if _, err := r.Create(request); err != nil {
		t.Fatal(err)
	}
	result, err := r.RestoreRehearsal("backup_1")
	if err != nil || result.State != "rehearsal_verified" || result.FileCount != int64(len(files)+1) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBackupRestoreRepositoryReadbackRejectsTypedJSONMismatch(t *testing.T) {
	tests := []struct {
		name string
		path string
		json string
	}{
		{name: "room", path: "rooms/room_1/room.json", json: `{"id":"room_1","sort_order":"not-an-integer"}`},
		{name: "device", path: "rooms/room_1/devices/device_1/device.json", json: `{"id":"device_1","capabilities":"not-an-array"}`},
		{name: "ir_config", path: "rooms/room_1/devices/device_1/adapters/ir/ir_config.json", json: `{"carrier_khz":"not-an-integer"}`},
		{name: "ir_state_schema", path: "rooms/room_1/devices/device_1/adapters/ir/state_schema.json", json: `{"mode":"not-an-object"}`},
		{name: "ir_command", path: "rooms/room_1/devices/device_1/adapters/ir/commands/power.json", json: `{"carrier_khz":"not-an-integer"}`},
		{name: "ir_state", path: "rooms/room_1/devices/device_1/adapters/ir/states/cool.json", json: `{"carrier_khz":"not-an-integer"}`},
		{name: "legacy_ir_signal", path: "ir-signals/test.json", json: `{"carrier_khz":"not-an-integer"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, c := backupFixture(t)
			path := filepath.Join(c.RuntimeDataDir, filepath.FromSlash(tc.path))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.json), 0600); err != nil {
				t.Fatal(err)
			}
			request := backupRequest("rehearsal")
			request.Sources = []string{tc.path}
			if _, err := r.Create(request); err != nil {
				t.Fatal(err)
			}
			if _, err := r.RestoreRehearsal("backup_1"); !errors.Is(err, ErrBackupCorrupt) {
				t.Fatalf("expected repository readback error, got %v", err)
			}
			loaded, err := r.Load("backup_1")
			if err != nil || loaded.Registry.State != "snapshot_verified" {
				t.Fatalf("invalid typed JSON promoted: registry=%+v err=%v", loaded.Registry, err)
			}
			target := filepath.Join(c.Dir, ".restore-rehearsal", "backup_1", filepath.FromSlash(tc.path))
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("failed rehearsal target should be preserved for inspection: %v", err)
			}
		})
	}
}

func TestBackupRestoreRepositoryReadbackRejectsUnmappedJSON(t *testing.T) {
	err := readBackupJSONThroughRepository(t.TempDir(), "system/settings.json")
	if err == nil || !strings.Contains(err.Error(), "no repository readback mapping") {
		t.Fatalf("unmapped JSON readback error=%v", err)
	}
}
