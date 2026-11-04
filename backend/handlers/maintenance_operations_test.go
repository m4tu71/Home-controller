package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type historyFixture struct {
	records []models.MaintenanceOperation
	err     error
}

func (f historyFixture) List() ([]models.MaintenanceOperation, error) { return f.records, f.err }
func (f historyFixture) Load(id string) (models.MaintenanceOperation, error) {
	if f.err != nil {
		return models.MaintenanceOperation{}, f.err
	}
	for _, v := range f.records {
		if v.ID == id {
			return v, nil
		}
	}
	return models.MaintenanceOperation{}, repositories.ErrMaintenanceMissing
}
func (f historyFixture) Update(models.MaintenanceOperation) error { return errors.New("not used") }

func TestMaintenanceHistoryRoutes(t *testing.T) {
	record := models.MaintenanceOperation{ID: "op", PlanID: "plan_1", Type: "migration", Status: "recovery_required", StartedAt: time.Now().UTC(), Target: "/private/internal/path", Error: "failed at /private/internal/path", Actions: []models.MaintenanceOperationAction{{ID: "a", Action: "/private/internal/action", Status: "applying", Error: "/private/internal/path"}}, Recovery: &models.MaintenanceRecovery{PreviousStatus: "applying", Actions: []models.MaintenanceRecoveryAction{{ActionID: "a", Classification: services.MaintenanceAmbiguous, Reason: "runtime_read_failed"}}}}
	h := &MaintenanceOperationsHandler{history: services.NewMaintenanceHistoryService(historyFixture{records: []models.MaintenanceOperation{record}})}
	for _, path := range []string{maintenanceOperationsPath, maintenanceOperationsPath + "/op"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/private/") {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, maintenanceOperationsPath+"/op", nil))
	if !strings.Contains(w.Body.String(), `"manual_inspection"`) {
		t.Fatal(w.Body.String())
	}
}

func TestMaintenanceHistoryReturnsOnlySafeTargetAndActionTokens(t *testing.T) {
	record := models.MaintenanceOperation{
		ID: "op_safe", PlanID: "plan_1", Type: "migration", Target: "roomlight", Status: "verified",
		StartedAt: time.Now().UTC(), Actions: []models.MaintenanceOperationAction{{ID: "action_1", Action: "write_ir_command", Status: "verified"}},
	}
	h := &MaintenanceOperationsHandler{history: services.NewMaintenanceHistoryService(historyFixture{records: []models.MaintenanceOperation{record}})}
	for _, path := range []string{maintenanceOperationsPath, maintenanceOperationsPath + "/op_safe"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"target":"roomlight"`) {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
		if path != maintenanceOperationsPath && !strings.Contains(w.Body.String(), `"action":"write_ir_command"`) {
			t.Fatalf("%s: action token missing: %s", path, w.Body.String())
		}
	}
}

func TestMaintenanceHistoryErrors(t *testing.T) {
	for _, tc := range []struct {
		name, path, method string
		history            maintenanceHistoryReader
		want               int
	}{
		{"unavailable", maintenanceOperationsPath, http.MethodGet, nil, http.StatusServiceUnavailable},
		{"missing", maintenanceOperationsPath + "/missing", http.MethodGet, services.NewMaintenanceHistoryService(historyFixture{}), http.StatusNotFound},
		{"corrupt", maintenanceOperationsPath, http.MethodGet, services.NewMaintenanceHistoryService(historyFixture{err: repositories.ErrMaintenanceCorrupt}), http.StatusServiceUnavailable},
		{"method", maintenanceOperationsPath, http.MethodPost, services.NewMaintenanceHistoryService(historyFixture{}), http.StatusMethodNotAllowed},
		{"nested", maintenanceOperationsPath + "/a/b", http.MethodGet, services.NewMaintenanceHistoryService(historyFixture{}), http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &MaintenanceOperationsHandler{history: tc.history}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
