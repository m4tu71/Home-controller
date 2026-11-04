package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type migrationApplyStub struct{}

func (migrationApplyStub) Apply(models.MigrationApplyRequest) (models.MaintenanceOperation, error) {
	return models.MaintenanceOperation{ID: "migration_test", Status: "verified"}, nil
}

func TestMigrationApplyHTTPStrictInput(t *testing.T) {
	h := NewMigrationApplyHandler(migrationApplyStub{})
	valid := `{"plan_id":"plan_0123456789abcdef0123456789abcdef","plan_digest":"sha256:x","action_ids":["migrate_power"],"rehearsal_backup_id":"backup_test","confirm":true}`
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"valid", http.MethodPost, valid, 200},
		{"unknown", http.MethodPost, valid[:len(valid)-1] + `,"extra":true}`, 400},
		{"trailing", http.MethodPost, valid + ` {}`, 400},
		{"unconfirmed", http.MethodPost, strings.Replace(valid, `"confirm":true`, `"confirm":false`, 1), 400},
		{"oversized", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit1MiB)+1), 413},
		{"method", http.MethodGet, "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, MigrationApplyRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
