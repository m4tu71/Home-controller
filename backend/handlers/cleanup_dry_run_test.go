package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type cleanupCreatorFixture struct {
	calls int
	err   error
	plan  models.MaintenancePlanEnvelope
}

func (f *cleanupCreatorFixture) Create([]models.CleanupIssueSelection) (models.MaintenancePlanEnvelope, error) {
	f.calls++
	return f.plan, f.err
}

func TestCleanupDryRunStrictInputAndErrors(t *testing.T) {
	hash := strings.Repeat("a", 64)
	good := `{"issues":[{"issue_id":"invalid_json:ir-signals/bad.json","before_hash":"` + hash + `"}]}`
	for _, tc := range []struct {
		name, method, path, contentType, body string
		service                               *cleanupCreatorFixture
		want, calls                           int
	}{
		{"wrong method", "GET", CleanupDryRunRoute, "application/json", good, &cleanupCreatorFixture{}, 405, 0},
		{"wrong path", "POST", CleanupDryRunRoute + "/extra", "application/json", good, &cleanupCreatorFixture{}, 404, 0},
		{"query", "POST", CleanupDryRunRoute + "?x=1", "application/json", good, &cleanupCreatorFixture{}, 400, 0},
		{"content type", "POST", CleanupDryRunRoute, "", good, &cleanupCreatorFixture{}, 415, 0},
		{"unknown field", "POST", CleanupDryRunRoute, "application/json", strings.TrimSuffix(good, "}") + `,"include_destructive":true}`, &cleanupCreatorFixture{}, 400, 0},
		{"trailing JSON", "POST", CleanupDryRunRoute, "application/json", good + `{}`, &cleanupCreatorFixture{}, 400, 0},
		{"empty selection", "POST", CleanupDryRunRoute, "application/json", `{"issues":[]}`, &cleanupCreatorFixture{}, 400, 0},
		{"duplicate", "POST", CleanupDryRunRoute, "application/json", `{"issues":[{"issue_id":"x","before_hash":"` + hash + `"},{"issue_id":"x","before_hash":"` + hash + `"}]}`, &cleanupCreatorFixture{}, 400, 0},
		{"oversize", "POST", CleanupDryRunRoute, "application/json", strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), &cleanupCreatorFixture{}, 413, 0},
		{"unavailable", "POST", CleanupDryRunRoute, "application/json", good, nil, 503, 0},
		{"missing", "POST", CleanupDryRunRoute, "application/json", good, &cleanupCreatorFixture{err: services.ErrCleanupIssueMissing}, 404, 1},
		{"stale", "POST", CleanupDryRunRoute, "application/json", good, &cleanupCreatorFixture{err: services.ErrCleanupIssueStale}, 409, 1},
		{"unsupported", "POST", CleanupDryRunRoute, "application/json", good, &cleanupCreatorFixture{err: services.ErrCleanupIssueUnsupported}, 400, 1},
		{"store failure", "POST", CleanupDryRunRoute, "application/json", good, &cleanupCreatorFixture{err: errors.New("private path")}, 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			NewCleanupDryRunHandler(tc.service).ServeHTTP(w, r)
			calls := 0
			if tc.service != nil {
				calls = tc.service.calls
			}
			if w.Code != tc.want || calls != tc.calls || strings.Contains(w.Body.String(), "private path") {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestCleanupDryRunRouteSavesSelectedPlanWithoutRuntimeMutation(t *testing.T) {
	base := t.TempDir()
	cfg := repositories.BackupConfig{Dir: filepath.Join(base, "backups"), RuntimeDataDir: filepath.Join(base, "data"), RepositoryDir: filepath.Join(base, "repo"), WebRootDir: filepath.Join(base, "web"), StateDir: filepath.Join(base, "state"), TrashDir: filepath.Join(base, "trash")}
	for _, p := range []string{cfg.Dir, cfg.RuntimeDataDir, cfg.RepositoryDir, cfg.WebRootDir, cfg.StateDir, filepath.Join(cfg.StateDir, "plans"), filepath.Join(cfg.StateDir, "operations"), cfg.TrashDir} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	put := func(rel, data string) {
		t.Helper()
		p := filepath.Join(cfg.RuntimeDataDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("ir-signals/bad.json", "{")
	put("ir-signals/untouched.json", `{"format":"raw_samples"}`)
	put("rooms/room/room.json", `{"id":"room"}`)
	put("rooms/room/devices/device/other.bin", "keep")
	backup, err := repositories.NewBackupRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: cfg.StateDir, RuntimeDataDir: cfg.RuntimeDataDir, RepositoryDir: cfg.RepositoryDir, WebRootDir: cfg.WebRootDir, BackupDir: cfg.Dir, TrashDir: cfg.TrashDir})
	if err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewDataHealthRepository(cfg.RuntimeDataDir)
	health := services.NewDataHealthService(repo)
	planService := services.NewMaintenancePlanService(state.Plans(), backup)
	creator := services.NewCleanupPlanService(health, planService)
	before, err := repo.Scan()
	if err != nil {
		t.Fatal(err)
	}
	issues, err := health.Scan()
	if err != nil {
		t.Fatal(err)
	}
	selected := []models.CleanupIssueSelection{}
	for _, issue := range issues {
		if issue.ID == "invalid_json:ir-signals/bad.json" || issue.ID == "missing_device_json:rooms/room/devices/device" {
			selected = append(selected, models.CleanupIssueSelection{IssueID: issue.ID, BeforeHash: issue.Hash})
		}
	}
	if len(selected) != 2 {
		t.Fatalf("selected=%+v", selected)
	}
	mux := http.NewServeMux()
	mux.Handle(CleanupDryRunRoute, NewCleanupDryRunHandler(creator))
	for _, tc := range []struct {
		name  string
		items []models.CleanupIssueSelection
		want  int
	}{
		{"unknown issue", []models.CleanupIssueSelection{{IssueID: "invalid_json:ir-signals/unknown.json", BeforeHash: selected[0].BeforeHash}}, 404},
		{"stale issue", []models.CleanupIssueSelection{{IssueID: selected[0].IssueID, BeforeHash: strings.Repeat("a", 64)}}, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(models.CleanupDryRunRequest{Issues: tc.items})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", CleanupDryRunRoute, strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	plansBefore, err := os.ReadDir(filepath.Join(cfg.StateDir, "plans"))
	if err != nil || len(plansBefore) != 0 {
		t.Fatalf("rejected requests saved plans: %d, %v", len(plansBefore), err)
	}
	request, _ := json.Marshal(models.CleanupDryRunRequest{Issues: selected})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", CleanupDryRunRoute, strings.NewReader(string(request)))
	r.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		OK                bool             `json:"ok"`
		PlanID            string           `json:"plan_id"`
		PlanDigest        string           `json:"plan_digest"`
		ReadyToApply      bool             `json:"ready_to_apply"`
		BackupPlanned     bool             `json:"backup_planned"`
		BackupRequirement string           `json:"backup_requirement"`
		Actions           []map[string]any `json:"actions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.ReadyToApply || !response.BackupPlanned || response.BackupRequirement != "apply" || len(response.Actions) != 2 || response.PlanID == "" || response.PlanDigest == "" {
		t.Fatalf("response=%+v", response)
	}
	for _, action := range response.Actions {
		if action["destructive"] != true || action["backup_inventory_hash"] == "" || action["expected_after_state"] != "moved_to_trash" {
			t.Fatalf("action=%+v", action)
		}
	}
	if _, err := creator.Load(response.PlanID); err != nil {
		t.Fatalf("saved plan: %v", err)
	}
	after, err := repo.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("runtime fixture changed")
	}
}
