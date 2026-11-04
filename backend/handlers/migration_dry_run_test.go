package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type migrationDryRunStub struct{}

func (migrationDryRunStub) Create(models.MigrationDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	return models.MaintenancePlanEnvelope{PlanID: "plan_test", Digest: "sha256:test", ReadyToApply: true}, nil
}

func TestMigrationDryRunHTTPStrictInput(t *testing.T) {
	h := NewMigrationDryRunHandler(migrationDryRunStub{})
	valid := `{"mappings":[{"action_id":"a","source_path":"ir-signals/a.json","target_kind":"command","room_id":"r","device_id":"d","target_id":"power"}]}`
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"valid", http.MethodPost, valid, http.StatusCreated},
		{"unknown", http.MethodPost, `{"mappings":[],"extra":true}`, http.StatusBadRequest},
		{"trailing", http.MethodPost, valid + ` {}`, http.StatusBadRequest},
		{"oversize", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit1MiB)+1), http.StatusRequestEntityTooLarge},
		{"method", http.MethodGet, "", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, MigrationDryRunRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
