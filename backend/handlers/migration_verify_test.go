package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type migrationVerifyStub struct{}

func (migrationVerifyStub) Verify(models.MigrationVerifyRequest) (models.MigrationVerifyResult, error) {
	return models.MigrationVerifyResult{OperationID: "migration_test", Status: "verified", Targets: []models.MigrationVerifyTarget{}}, nil
}

func TestMigrationVerifyHTTPStrictInput(t *testing.T) {
	h := NewMigrationVerifyHandler(migrationVerifyStub{})
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"valid", http.MethodPost, `{"operation_id":"migration_test"}`, 200},
		{"unknown", http.MethodPost, `{"operation_id":"migration_test","extra":true}`, 400},
		{"trailing", http.MethodPost, `{"operation_id":"migration_test"} {}`, 400},
		{"missing", http.MethodPost, `{}`, 400},
		{"oversized", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), 413},
		{"method", http.MethodGet, "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, MigrationVerifyRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
