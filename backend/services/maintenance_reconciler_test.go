package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
)

func reconcileFixture(t *testing.T) (*repositories.MaintenanceState, MaintenanceFilesystemInspector, string, string) {
	t.Helper()
	base := t.TempDir()
	stateRoot := filepath.Join(base, "state")
	dataRoot := filepath.Join(base, "data")
	trashRoot := filepath.Join(base, "trash")
	for _, dir := range []string{stateRoot, filepath.Join(stateRoot, "plans"), filepath.Join(stateRoot, "operations"), dataRoot, trashRoot} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: stateRoot, RuntimeDataDir: dataRoot, TrashDir: trashRoot})
	if err != nil {
		t.Fatal(err)
	}
	return state, MaintenanceFilesystemInspector{RuntimeRoot: dataRoot, TrashRoot: trashRoot}, dataRoot, trashRoot
}

func snapshotJSON(t *testing.T, runtime, trash *models.MaintenanceSnapshotEntry) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(models.MaintenanceSnapshot{Runtime: runtime, Trash: trash})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fileDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func TestMaintenanceStartupInterruptionFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		after        bool
		trash        bool
		want         string
	}{
		{"prepared", "prepared", false, false, MaintenanceUnchanged},
		{"action_applying_before", "applying", false, false, MaintenanceUnchanged},
		{"mutation_after_before_record", "applying", true, false, MaintenanceAppliedUnconfirmed},
		{"partial_action", "partial_failure", true, false, MaintenanceAppliedUnconfirmed},
		{"verifying", "verifying", true, false, MaintenanceAppliedUnconfirmed},
		{"trash_mutation", "applying", true, true, MaintenanceTrashedUnconfirmed},
		{"restoring", "restoring", true, false, MaintenanceAppliedUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, inspector, dataRoot, trashRoot := reconcileFixture(t)
			beforeData := []byte("before")
			afterData := []byte("after")
			beforeRuntime := &models.MaintenanceSnapshotEntry{Path: "target.json", Exists: true, Kind: "file", SHA256: fileDigest(beforeData)}
			intendedRuntime := &models.MaintenanceSnapshotEntry{Path: "target.json", Exists: true, Kind: "file", SHA256: fileDigest(afterData)}
			var beforeTrash, intendedTrash *models.MaintenanceSnapshotEntry
			if tc.trash {
				intendedRuntime = &models.MaintenanceSnapshotEntry{Path: "target.json"}
				beforeTrash = &models.MaintenanceSnapshotEntry{Path: "entry.json"}
				intendedTrash = &models.MaintenanceSnapshotEntry{Path: "entry.json", Exists: true, Kind: "file", SHA256: fileDigest(beforeData)}
			}
			if tc.name == "restoring" {
				beforeRuntime = &models.MaintenanceSnapshotEntry{Path: "target.json"}
				beforeTrash = &models.MaintenanceSnapshotEntry{Path: "entry.json", Exists: true, Kind: "file", SHA256: fileDigest(afterData)}
				intendedTrash = &models.MaintenanceSnapshotEntry{Path: "entry.json"}
			}
			if tc.after {
				if tc.trash {
					if err := os.WriteFile(filepath.Join(trashRoot, "entry.json"), beforeData, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(filepath.Join(dataRoot, "target.json"), afterData, 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if err := os.WriteFile(filepath.Join(dataRoot, "target.json"), beforeData, 0600); err != nil {
					t.Fatal(err)
				}
			}
			actionStatus := "applying"
			if tc.status == "prepared" {
				actionStatus = "pending"
			}
			if tc.status == "verifying" {
				actionStatus = "applied"
			}
			record := models.MaintenanceOperation{Version: 1, ID: "op", Revision: 1, StartedAt: time.Now().UTC(), Target: "room", Status: tc.status, Actions: []models.MaintenanceOperationAction{{ID: "a", Action: "write", Status: actionStatus, BeforeState: snapshotJSON(t, beforeRuntime, beforeTrash), IntendedState: snapshotJSON(t, intendedRuntime, intendedTrash)}}}
			if err := state.Operations().Create(record); err != nil {
				t.Fatal(err)
			}
			if err := NewMaintenanceReconciler(state.Operations(), inspector).Scan(); err != nil {
				t.Fatal(err)
			}
			got, err := state.Operations().Load("op")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "recovery_required" || got.Recovery == nil || got.Recovery.PreviousStatus != tc.status || len(got.Recovery.Actions) != 1 || got.Recovery.Actions[0].Classification != tc.want {
				t.Fatalf("record: %+v", got)
			}
			if err := NewMaintenanceReconciler(state.Operations(), inspector).Scan(); err != nil {
				t.Fatal(err)
			}
			again, err := state.Operations().Load("op")
			if err != nil || again.Revision != 2 {
				t.Fatalf("duplicate reconcile: %+v %v", again, err)
			}
		})
	}
}

func TestMaintenanceStartupBeforeRecordAndAmbiguous(t *testing.T) {
	state, inspector, dataRoot, _ := reconcileFixture(t)
	if err := NewMaintenanceReconciler(state.Operations(), inspector).Scan(); err != nil {
		t.Fatal(err)
	}
	list, err := state.Operations().List()
	if err != nil || len(list) != 0 {
		t.Fatalf("before record: %+v %v", list, err)
	}
	path := filepath.Join(dataRoot, "target.json")
	if err := os.WriteFile(path, []byte("neither"), 0600); err != nil {
		t.Fatal(err)
	}
	before := snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "target.json", Exists: true, Kind: "file", SHA256: fileDigest([]byte("before"))}, nil)
	intended := snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "target.json", Exists: true, Kind: "file", SHA256: fileDigest([]byte("after"))}, nil)
	record := models.MaintenanceOperation{Version: 1, ID: "op", Revision: 1, StartedAt: time.Now().UTC(), Target: "room", Status: "applying", Actions: []models.MaintenanceOperationAction{{ID: "a", Action: "write", Status: "applying", BeforeState: before, IntendedState: intended}}}
	if err := state.Operations().Create(record); err != nil {
		t.Fatal(err)
	}
	if err := NewMaintenanceReconciler(state.Operations(), inspector).Scan(); err != nil {
		t.Fatal(err)
	}
	got, err := state.Operations().Load("op")
	if err != nil || got.Recovery.Actions[0].Classification != MaintenanceAmbiguous {
		t.Fatalf("ambiguous: %+v %v", got, err)
	}
}

func TestMaintenanceInspectorRejectsTraversalAndSymlink(t *testing.T) {
	_, inspector, dataRoot, _ := reconcileFixture(t)
	for _, rel := range []string{"../secret", "/etc/passwd"} {
		a := models.MaintenanceOperationAction{ID: "a", BeforeState: snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: rel}, nil), IntendedState: snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: rel, Exists: true, Kind: "file", SHA256: fileDigest([]byte("x"))}, nil)}
		if got := inspector.Inspect(a); got.Classification != MaintenanceAmbiguous {
			t.Fatalf("unsafe %s: %+v", rel, got)
		}
	}
	if err := os.Symlink(filepath.Join(dataRoot, "missing"), filepath.Join(dataRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	a := models.MaintenanceOperationAction{ID: "a", BeforeState: snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "linked"}, nil), IntendedState: snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "linked", Exists: true, Kind: "file", SHA256: fileDigest([]byte("x"))}, nil)}
	if got := inspector.Inspect(a); got.Classification != MaintenanceAmbiguous {
		t.Fatalf("symlink: %+v", got)
	}
}

type failingHistoryStore struct{ record models.MaintenanceOperation }

func (s *failingHistoryStore) List() ([]models.MaintenanceOperation, error) {
	return []models.MaintenanceOperation{s.record}, nil
}
func (s *failingHistoryStore) Load(string) (models.MaintenanceOperation, error) { return s.record, nil }
func (s *failingHistoryStore) Update(models.MaintenanceOperation) error {
	return errors.New("injected record update failure")
}

func TestMaintenanceStartupRecordUpdateFailureStops(t *testing.T) {
	s := &failingHistoryStore{record: models.MaintenanceOperation{ID: "op", Status: "prepared", Actions: []models.MaintenanceOperationAction{{ID: "a"}}}}
	if err := NewMaintenanceReconciler(s, MaintenanceFilesystemInspector{}).Scan(); err == nil {
		t.Fatal("expected update failure")
	}
	if s.record.Status != "prepared" {
		t.Fatal("failed update changed stored status")
	}
}

func TestMaintenanceStartupMixedPartialActions(t *testing.T) {
	state, inspector, dataRoot, _ := reconcileFixture(t)
	var actions []models.MaintenanceOperationAction
	for _, tc := range []struct{ id, content, status string }{{"a", "after", "applied"}, {"b", "before", "pending"}} {
		path := tc.id + ".json"
		if err := os.WriteFile(filepath.Join(dataRoot, path), []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, models.MaintenanceOperationAction{ID: tc.id, Action: "write", Status: tc.status,
			BeforeState:   snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: path, Exists: true, Kind: "file", SHA256: fileDigest([]byte("before"))}, nil),
			IntendedState: snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: path, Exists: true, Kind: "file", SHA256: fileDigest([]byte("after"))}, nil)})
	}
	record := models.MaintenanceOperation{Version: 1, ID: "op", Revision: 1, StartedAt: time.Now().UTC(), Target: "room", Status: "partial_failure", Actions: actions}
	if err := state.Operations().Create(record); err != nil {
		t.Fatal(err)
	}
	if err := NewMaintenanceReconciler(state.Operations(), inspector).Scan(); err != nil {
		t.Fatal(err)
	}
	got, err := state.Operations().Load("op")
	if err != nil || got.Recovery.Actions[0].Classification != MaintenanceAppliedUnconfirmed || got.Recovery.Actions[1].Classification != MaintenanceUnchanged {
		t.Fatalf("mixed partial: %+v %v", got, err)
	}
}

func TestMaintenanceInspectorDirectoryAndNestedSymlink(t *testing.T) {
	_, inspector, dataRoot, _ := reconcileFixture(t)
	dir := filepath.Join(dataRoot, "bundle")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "item.json"), []byte(`{"x":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := hashObservedDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "bundle"}, nil)
	intended := snapshotJSON(t, &models.MaintenanceSnapshotEntry{Path: "bundle", Exists: true, Kind: "directory", SHA256: digest}, nil)
	a := models.MaintenanceOperationAction{ID: "a", BeforeState: before, IntendedState: intended}
	if got := inspector.Inspect(a); got.Classification != MaintenanceAppliedUnconfirmed {
		t.Fatalf("directory: %+v", got)
	}
	if err := os.Symlink(filepath.Join(dataRoot, "outside"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if got := inspector.Inspect(a); got.Classification != MaintenanceAmbiguous {
		t.Fatalf("nested symlink: %+v", got)
	}
}
