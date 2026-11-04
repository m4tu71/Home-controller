package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

type backupDetailFixture struct {
	detail models.BackupDetail
	err    error
}

func (f backupDetailFixture) Detail(string) (models.BackupDetail, error) { return f.detail, f.err }

type backupRestoreHTTPFixture struct {
	backupDetailFixture
	preview    repositories.BackupRestorePreview
	result     repositories.BackupRestoreResult
	restoreErr error
	calls      int
}

func (f *backupRestoreHTTPFixture) RestoreDryRun(string) (repositories.BackupRestorePreview, error) {
	f.calls++
	return f.preview, f.restoreErr
}
func (f *backupRestoreHTTPFixture) RestoreRehearsal(string) (repositories.BackupRestoreResult, error) {
	f.calls++
	return f.result, f.restoreErr
}

func TestBackupDetailHandler(t *testing.T) {
	for _, tc := range []struct {
		name, path, method string
		fixture            backupDetailReader
		want               int
	}{
		{"success", backupPath + "backup_1", http.MethodGet, backupDetailFixture{detail: models.BackupDetail{Bundle: models.BackupBundle{ID: "backup_1", FileCount: 1}, Registry: models.BackupRegistry{ID: "backup_1", Role: "apply", State: "point_in_time_verified"}}}, 200},
		{"missing", backupPath + "missing", http.MethodGet, backupDetailFixture{err: repositories.ErrBackupMissing}, 404},
		{"corrupt", backupPath + "backup_1", http.MethodGet, backupDetailFixture{err: repositories.ErrBackupCorrupt}, 503},
		{"unavailable", backupPath + "backup_1", http.MethodGet, nil, 503},
		{"invalid ID", backupPath + "%20", http.MethodGet, backupDetailFixture{}, 400},
		{"nested", backupPath + "backup_1/restore", http.MethodGet, backupDetailFixture{}, 404},
		{"create not registered", backupPath + "backup_1", http.MethodPost, backupDetailFixture{}, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &BackupDetailHandler{reader: tc.fixture}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	r := httptest.NewRequest(http.MethodPost, backupPath+"backup_1/restore/rehearsal", strings.NewReader("chunked"))
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	f := &backupRestoreHTTPFixture{}
	w := httptest.NewRecorder()
	(&BackupDetailHandler{reader: f, restorer: f}).ServeHTTP(w, r)
	if w.Code != 400 || f.calls != 0 {
		t.Fatalf("chunked body status=%d calls=%d body=%s", w.Code, f.calls, w.Body.String())
	}
}

func TestBackupRestoreHTTPBodyAndRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, path, method, body string
		want, calls              int
	}{
		{"dry-run", backupPath + "backup_1/restore/dry-run", http.MethodPost, "", 200, 1},
		{"rehearsal", backupPath + "backup_1/restore/rehearsal", http.MethodPost, "", 200, 1},
		{"object rejected", backupPath + "backup_1/restore/dry-run", http.MethodPost, "{}", 400, 0},
		{"whitespace rejected", backupPath + "backup_1/restore/rehearsal", http.MethodPost, " ", 400, 0},
		{"oversize rejected", backupPath + "backup_1/restore/dry-run", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), 413, 0},
		{"create route absent", backupPath + "backup_1", http.MethodPost, "", 404, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &backupRestoreHTTPFixture{preview: repositories.BackupRestorePreview{BackupID: "backup_1", CanRehearse: true}, result: repositories.BackupRestoreResult{BackupID: "backup_1", State: "rehearsal_verified"}}
			h := &BackupDetailHandler{reader: f, restorer: f}
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || f.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body.String())
			}
		})
	}
}

func TestBackupRestoreHTTPConflicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		preview repositories.BackupRestorePreview
		err     error
		want    int
	}{
		{"unverified", repositories.BackupRestorePreview{Reason: "backup_not_rehearsal_candidate"}, nil, 409},
		{"wrong role", repositories.BackupRestorePreview{Reason: "wrong_backup_role"}, nil, 409},
		{"missing", repositories.BackupRestorePreview{}, repositories.ErrBackupMissing, 404},
		{"corrupt", repositories.BackupRestorePreview{}, repositories.ErrBackupCorrupt, 503},
		{"insufficient capacity", repositories.BackupRestorePreview{}, repositories.ErrBackupCapacity, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &backupRestoreHTTPFixture{preview: tc.preview, restoreErr: tc.err}
			h := &BackupDetailHandler{reader: f, restorer: f}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, backupPath+"backup_1/restore/dry-run", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
