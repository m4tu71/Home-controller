package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

func cleanupFixture(t *testing.T) (maintenancePlanFixture, *CleanupPlanService, func(string, string)) {
	t.Helper()
	f := newMaintenancePlanFixture(t)
	put := func(rel, data string) {
		t.Helper()
		p := filepath.Join(f.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	health := NewDataHealthService(repositories.NewDataHealthRepository(f.root))
	return f, NewCleanupPlanService(health, f.service), put
}

func selectedIssue(t *testing.T, service *CleanupPlanService, id string) models.CleanupIssueSelection {
	t.Helper()
	issues, err := service.health.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range issues {
		if issue.ID == id {
			return models.CleanupIssueSelection{IssueID: id, BeforeHash: issue.Hash}
		}
	}
	t.Fatalf("issue not found: %s", id)
	return models.CleanupIssueSelection{}
}

func TestCleanupPlanCreateSelectedImmutableActions(t *testing.T) {
	f, service, put := cleanupFixture(t)
	put("ir-signals/broken.json", "{")
	put("rooms/valid/room.json", `{"id":"valid"}`)
	put("rooms/valid/devices/without/device-adapter.bin", "keep")
	before, err := service.health.Repository.Scan()
	if err != nil {
		t.Fatal(err)
	}
	selected := []models.CleanupIssueSelection{
		selectedIssue(t, service, "invalid_json:ir-signals/broken.json"),
		selectedIssue(t, service, "missing_device_json:rooms/valid/devices/without"),
	}
	plan, err := service.Create(selected)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OperationType != "cleanup" || plan.PayloadSchemaID != "cleanup.v1" || plan.BackupRequirement != "apply" || !plan.ReadyToApply || len(plan.Actions) != 2 || plan.PlanID == "" || plan.Digest == "" {
		t.Fatalf("plan=%+v", plan)
	}
	var payload models.CleanupPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Actions) != 2 {
		t.Fatalf("payload=%+v", payload)
	}
	for _, action := range payload.Actions {
		if action.ActionID != cleanupActionID(action.IssueID) || !action.Destructive || action.ExpectedAfterState != "moved_to_trash" || len(action.AffectedPaths) != 1 || action.BeforeHash == "" {
			t.Fatalf("action=%+v", action)
		}
	}
	loaded, err := service.Load(plan.PlanID)
	if err != nil || loaded.Digest != plan.Digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	selection, err := f.service.VerifyApplySelection(plan.PlanID, plan.Digest, []string{payload.Actions[0].ActionID})
	if err != nil || len(selection.Sources) != 1 {
		t.Fatalf("backup selection=%+v err=%v", selection, err)
	}
	after, err := service.health.Repository.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("runtime fixture changed")
	}
}

func TestCleanupPlanRejectsStaleDuplicateUnknownAndOverlap(t *testing.T) {
	_, service, put := cleanupFixture(t)
	put("ir-signals/broken.json", "{")
	put("rooms/lost/devices/device/device.json", `{"id":"device"}`)
	broken := selectedIssue(t, service, "invalid_json:ir-signals/broken.json")
	room := selectedIssue(t, service, "missing_room_json:rooms/lost")
	device := selectedIssue(t, service, "orphan_device:rooms/lost/devices/device")
	for _, tc := range []struct {
		name  string
		items []models.CleanupIssueSelection
		want  error
	}{
		{"empty", nil, ErrCleanupSelectionInvalid},
		{"duplicate", []models.CleanupIssueSelection{broken, broken}, ErrCleanupSelectionInvalid},
		{"unknown", []models.CleanupIssueSelection{{IssueID: "invalid_json:ir-signals/nope.json", BeforeHash: broken.BeforeHash}}, ErrCleanupIssueMissing},
		{"overlap", []models.CleanupIssueSelection{room, device}, ErrCleanupSelectionInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Create(tc.items); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
	put("ir-signals/broken.json", "{{")
	if _, err := service.Create([]models.CleanupIssueSelection{broken}); !errors.Is(err, ErrCleanupIssueStale) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestCleanupPlanRejectsNonCleanupOutsideRootAndSymlink(t *testing.T) {
	f, service, put := cleanupFixture(t)
	put("rooms/good/room.json", `{"id":"good"}`)
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.root, "linked.json")); err != nil {
		t.Fatal(err)
	}
	link := selectedIssue(t, service, "unsafe_symlink:linked.json")
	if _, err := service.Create([]models.CleanupIssueSelection{link}); !errors.Is(err, ErrCleanupIssueUnsupported) {
		t.Fatalf("symlink err=%v", err)
	}
	root := f.root
	for _, tc := range []struct {
		name  string
		issue models.DataHealthIssue
		want  error
	}{
		{"outside", models.DataHealthIssue{ID: "invalid_json:../outside.json", Path: "data/../outside.json", CanonicalPath: outside, Hash: strings.Repeat("a", 64), HasDestructiveActions: true}, ErrCleanupSelectionInvalid},
		{"noncleanup", models.DataHealthIssue{ID: "empty_directory:rooms", Path: "data/rooms", CanonicalPath: filepath.Join(root, "rooms"), Hash: strings.Repeat("a", 64), HasDestructiveActions: true}, ErrCleanupIssueUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := cleanupActionForIssue(root, tc.issue)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestCleanupPlanRejectsNonCleanupRootIssue(t *testing.T) {
	f, service, _ := cleanupFixture(t)
	if err := os.Mkdir(filepath.Join(f.root, "rooms"), 0700); err != nil {
		t.Fatal(err)
	}
	rootIssue := selectedIssue(t, service, "empty_directory:rooms")
	if _, err := service.Create([]models.CleanupIssueSelection{rootIssue}); !errors.Is(err, ErrCleanupIssueUnsupported) {
		t.Fatalf("root cleanup err=%v", err)
	}
}

func TestCleanupPlanValidatorRejectsTamperedPayload(t *testing.T) {
	_, service, put := cleanupFixture(t)
	put("ir-signals/broken.json", "{")
	plan, err := service.Create([]models.CleanupIssueSelection{selectedIssue(t, service, "invalid_json:ir-signals/broken.json")})
	if err != nil {
		t.Fatal(err)
	}
	plan.Payload = json.RawMessage(strings.Replace(string(plan.Payload), "moved_to_trash", "deleted", 1))
	if err := ValidateCleanupPlan(plan); !errors.Is(err, ErrMaintenancePlanInvalid) {
		t.Fatalf("tampered err=%v", err)
	}
}
