package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type rollbackDryRunStub struct{}

func (rollbackDryRunStub) Create(models.MigrationRollbackDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	return models.MaintenancePlanEnvelope{PlanID: "plan_test", Payload: []byte(`{"targets":[]}`)}, nil
}

func TestMigrationRollbackDryRunHTTPStrictInput(t *testing.T) {
	h := NewMigrationRollbackDryRunHandler(rollbackDryRunStub{})
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"valid", http.MethodPost, `{"operation_id":"migration_test"}`, 201},
		{"unknown", http.MethodPost, `{"operation_id":"migration_test","extra":true}`, 400},
		{"trailing", http.MethodPost, `{"operation_id":"migration_test"} {}`, 400},
		{"missing", http.MethodPost, `{}`, 400},
		{"oversized", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit1MiB)+1), 413},
		{"method", http.MethodGet, "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, MigrationRollbackDryRunRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
