package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type backupCreateHTTPFixture struct {
	request models.BackupCreateRequest
	result  models.BackupDetail
	err     error
	calls   int
}

func (f *backupCreateHTTPFixture) CreateRehearsal(request models.BackupCreateRequest) (models.BackupDetail, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func TestBackupCreateHandlerStrictRequestAndResponse(t *testing.T) {
	body := `{"plan_id":"plan_1","plan_digest":"sha256:abc","action_ids":["action_1"]}`
	f := &backupCreateHTTPFixture{result: models.BackupDetail{
		Bundle:   models.BackupBundle{ID: "backup_1", PlanID: "plan_1", ActionIDs: []string{"action_1"}, FileCount: 2, TotalSize: 11, InventoryHash: "sha256:inventory"},
		Registry: models.BackupRegistry{ID: "backup_1", Role: "rehearsal", State: "snapshot_verified"},
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, BackupCreateRoute, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	NewBackupCreateHandler(f).ServeHTTP(w, r)
	if w.Code != http.StatusCreated || f.calls != 1 || f.request.PlanID != "plan_1" || f.request.PlanDigest != "sha256:abc" || len(f.request.ActionIDs) != 1 {
		t.Fatalf("status=%d calls=%d request=%+v body=%s", w.Code, f.calls, f.request, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"state":"snapshot_verified"`) || strings.Contains(w.Body.String(), "sources") {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}

func TestBackupCreateHandlerInputBoundary(t *testing.T) {
	baseBody := `{"plan_id":"plan_1","plan_digest":"sha256:abc","action_ids":["action_1"]}`
	for _, tc := range []struct {
		name        string
		path        string
		method      string
		contentType string
		body        string
		want        int
		calls       int
	}{
		{name: "wrong method", path: BackupCreateRoute, method: http.MethodGet, contentType: "application/json", body: baseBody, want: 405},
		{name: "wrong path", path: BackupCreateRoute + "/", method: http.MethodPost, contentType: "application/json", body: baseBody, want: 404},
		{name: "missing content type", path: BackupCreateRoute, method: http.MethodPost, body: baseBody, want: 415},
		{name: "malformed", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: "{", want: 400},
		{name: "unknown field", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: strings.TrimSuffix(baseBody, "}") + `,"path":"/tmp/x"}`, want: 400},
		{name: "trailing JSON", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: baseBody + `{}`, want: 400},
		{name: "empty actions", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: `{"plan_id":"plan_1","plan_digest":"sha256:abc","action_ids":[]}`, want: 400},
		{name: "duplicate actions", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: `{"plan_id":"plan_1","plan_digest":"sha256:abc","action_ids":["action_1","action_1"]}`, want: 400},
		{name: "oversize", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), want: 413},
		{name: "success", path: BackupCreateRoute, method: http.MethodPost, contentType: "application/json", body: baseBody, want: 201, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &backupCreateHTTPFixture{}
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			NewBackupCreateHandler(f).ServeHTTP(w, r)
			if w.Code != tc.want || f.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body.String())
			}
		})
	}
}

func TestBackupCreateHandlerVerifierErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "unknown plan", err: repositories.ErrMaintenanceMissing, want: 404},
		{name: "unknown action", err: services.ErrMaintenancePlanActionMissing, want: 404},
		{name: "invalid selection", err: services.ErrMaintenancePlanInvalid, want: 400},
		{name: "stale digest or source", err: services.ErrMaintenancePlanStale, want: 409},
		{name: "not ready", err: services.ErrMaintenancePlanNotReady, want: 409},
		{name: "insufficient capacity", err: repositories.ErrBackupCapacity, want: 503},
		{name: "corrupt plan", err: repositories.ErrMaintenanceCorrupt, want: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &backupCreateHTTPFixture{err: tc.err}
			r := httptest.NewRequest(http.MethodPost, BackupCreateRoute, strings.NewReader(`{"plan_id":"plan_1","plan_digest":"sha256:x","action_ids":["action_1"]}`))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			NewBackupCreateHandler(f).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	writeBackupCreateError(w, errors.New("unexpected"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected error status=%d", w.Code)
	}
}
