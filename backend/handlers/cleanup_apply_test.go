package handlers

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type cleanupApplierFixture struct {
	calls  int
	result models.MaintenanceOperation
	err    error
}

func (f *cleanupApplierFixture) Apply(models.CleanupApplyRequest) (models.MaintenanceOperation, error) {
	f.calls++
	return f.result, f.err
}

func TestCleanupApplyHandlerStrictBoundaryAndResponse(t *testing.T) {
	hash := strings.Repeat("a", 64)
	base := `{"plan_id":"plan_0123456789abcdef0123456789abcdef","plan_digest":"sha256:` + hash + `","issues":[{"issue_id":"invalid_json:ir-signals/bad.json","before_hash":"` + hash + `"}],"action_ids":["cleanup_one"],"confirm":true}`
	for _, tc := range []struct {
		name, method, path, contentType, body string
		service                               *cleanupApplierFixture
		want, calls                           int
	}{
		{"wrong method", "GET", CleanupApplyRoute, "application/json", base, &cleanupApplierFixture{}, 405, 0},
		{"wrong path", "POST", CleanupApplyRoute + "/x", "application/json", base, &cleanupApplierFixture{}, 404, 0},
		{"query", "POST", CleanupApplyRoute + "?all=true", "application/json", base, &cleanupApplierFixture{}, 400, 0},
		{"missing content type", "POST", CleanupApplyRoute, "", base, &cleanupApplierFixture{}, 415, 0},
		{"unknown field", "POST", CleanupApplyRoute, "application/json", strings.TrimSuffix(base, "}") + `,"path":"/tmp"}`, &cleanupApplierFixture{}, 400, 0},
		{"trailing JSON", "POST", CleanupApplyRoute, "application/json", base + `{}`, &cleanupApplierFixture{}, 400, 0},
		{"missing issue", "POST", CleanupApplyRoute, "application/json", `{"plan_id":"x","plan_digest":"x","issues":[],"action_ids":["x"],"confirm":true}`, &cleanupApplierFixture{}, 400, 0},
		{"confirm false", "POST", CleanupApplyRoute, "application/json", strings.Replace(base, `"confirm":true`, `"confirm":false`, 1), &cleanupApplierFixture{}, 400, 0},
		{"oversize", "POST", CleanupApplyRoute, "application/json", strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), &cleanupApplierFixture{}, 413, 0},
		{"unavailable", "POST", CleanupApplyRoute, "application/json", base, nil, 503, 0},
		{"stale", "POST", CleanupApplyRoute, "application/json", base, &cleanupApplierFixture{err: services.ErrCleanupIssueStale}, 409, 1},
		{"missing plan", "POST", CleanupApplyRoute, "application/json", base, &cleanupApplierFixture{err: repositories.ErrMaintenanceMissing}, 404, 1},
		{"backup failure", "POST", CleanupApplyRoute, "application/json", base, &cleanupApplierFixture{result: models.MaintenanceOperation{ID: "cleanup_operation", Status: "partial_failure"}, err: errors.New("private backup path")}, 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			NewCleanupApplyHandler(tc.service).ServeHTTP(w, r)
			calls := 0
			if tc.service != nil {
				calls = tc.service.calls
			}
			if w.Code != tc.want || calls != tc.calls || strings.Contains(w.Body.String(), "private backup path") {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
			}
		})
	}
	f := &cleanupApplierFixture{result: models.MaintenanceOperation{ID: "cleanup_operation", Status: "verified", Result: "verified", ApplyBackupID: "backup_id", Actions: []models.MaintenanceOperationAction{{ID: "cleanup_one", Status: "verified", IntendedState: []byte(`{"trash":{"path":"trash_entry/payload","exists":true,"kind":"file","sha256":"abc"}}`)}}}}
	r := httptest.NewRequest("POST", CleanupApplyRoute, strings.NewReader(base))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NewCleanupApplyHandler(f).ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"trash_entry_id":"trash_entry"`) || !strings.Contains(w.Body.String(), `"apply_backup_id":"backup_id"`) {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
}
