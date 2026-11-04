package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type rollbackApplyStub struct{}

func (rollbackApplyStub) Apply(models.MigrationRollbackApplyRequest) (models.MaintenanceOperation, error) {
	return models.MaintenanceOperation{ID: "rollback_test", Status: "verified"}, nil
}

func TestMigrationRollbackApplyHTTPStrictInput(t *testing.T) {
	h := NewMigrationRollbackApplyHandler(rollbackApplyStub{})
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"valid", http.MethodPost, `{"plan_id":"plan_test","plan_digest":"hash","action_ids":["migrate_raw"],"confirm":true}`, 200},
		{"unknown", http.MethodPost, `{"plan_id":"plan_test","plan_digest":"hash","action_ids":["migrate_raw"],"confirm":true,"extra":1}`, 400},
		{"trailing", http.MethodPost, `{"plan_id":"plan_test","plan_digest":"hash","action_ids":["migrate_raw"],"confirm":true}{}`, 400},
		{"oversized", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit1MiB)+1), 413},
		{"method", http.MethodGet, "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, MigrationRollbackApplyRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
