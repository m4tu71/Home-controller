package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type maintenanceApplyContractStub struct {
	operation models.MaintenanceOperation
	err       error
}

func (s maintenanceApplyContractStub) Apply(models.MigrationApplyRequest) (models.MaintenanceOperation, error) {
	return s.operation, s.err
}

type cleanupApplyContractStub struct {
	operation models.MaintenanceOperation
	err       error
}

func (s cleanupApplyContractStub) Apply(models.CleanupApplyRequest) (models.MaintenanceOperation, error) {
	return s.operation, s.err
}

type rollbackApplyContractStub struct {
	operation models.MaintenanceOperation
	err       error
}

func (s rollbackApplyContractStub) Apply(models.MigrationRollbackApplyRequest) (models.MaintenanceOperation, error) {
	return s.operation, s.err
}

func TestMaintenanceApplyFailureContractAcrossFamilies(t *testing.T) {
	cleanupHash := strings.Repeat("a", 64)
	routes := []struct {
		name, path, body string
		invalid          error
		handler          func(models.MaintenanceOperation, error) http.Handler
	}{
		{
			name: "migration apply", path: MigrationApplyRoute,
			body:    `{"plan_id":"plan_0123456789abcdef0123456789abcdef","plan_digest":"sha256:x","action_ids":["migrate_power"],"rehearsal_backup_id":"backup_test","confirm":true}`,
			invalid: services.ErrMigrationApplyInvalid,
			handler: func(operation models.MaintenanceOperation, err error) http.Handler {
				return NewMigrationApplyHandler(maintenanceApplyContractStub{operation: operation, err: err})
			},
		},
		{
			name: "cleanup apply", path: CleanupApplyRoute,
			body:    `{"plan_id":"plan_0123456789abcdef0123456789abcdef","plan_digest":"sha256:` + cleanupHash + `","issues":[{"issue_id":"invalid_json:ir-signals/bad.json","before_hash":"` + cleanupHash + `"}],"action_ids":["cleanup_one"],"confirm":true}`,
			invalid: services.ErrCleanupSelectionInvalid,
			handler: func(operation models.MaintenanceOperation, err error) http.Handler {
				return NewCleanupApplyHandler(cleanupApplyContractStub{operation: operation, err: err})
			},
		},
		{
			name: "migration rollback apply", path: MigrationRollbackApplyRoute,
			body:    `{"plan_id":"plan_test","plan_digest":"sha256:x","action_ids":["rollback_power"],"confirm":true}`,
			invalid: services.ErrMigrationRollbackInvalid,
			handler: func(operation models.MaintenanceOperation, err error) http.Handler {
				return NewMigrationRollbackApplyHandler(rollbackApplyContractStub{operation: operation, err: err})
			},
		},
	}
	scenarios := []struct {
		name       string
		err        func(error) error
		status     int
		operation  models.MaintenanceOperation
		wantFields bool
	}{
		{name: "invalid selection", err: func(invalid error) error { return invalid }, status: http.StatusBadRequest},
		{name: "invalid stored plan", err: func(error) error { return services.ErrMaintenancePlanInvalid }, status: http.StatusBadRequest},
		{name: "missing plan", err: func(error) error { return repositories.ErrMaintenanceMissing }, status: http.StatusNotFound},
		{name: "missing action", err: func(error) error { return services.ErrMaintenancePlanActionMissing }, status: http.StatusNotFound},
		{name: "stale plan", err: func(error) error { return services.ErrMaintenancePlanStale }, status: http.StatusConflict},
		{name: "exclusive claim", err: func(error) error { return repositories.ErrMaintenancePlanClaimed }, status: http.StatusConflict},
		{name: "corrupt state", err: func(error) error { return repositories.ErrMaintenanceCorrupt }, status: http.StatusServiceUnavailable},
		{name: "backup failure with recovery metadata", err: func(error) error { return errors.New("private backup path") }, status: http.StatusServiceUnavailable, operation: models.MaintenanceOperation{ID: "operation_test", Status: "recovery_required", ApplyBackupID: "backup_apply"}, wantFields: true},
	}

	for _, route := range routes {
		for _, scenario := range scenarios {
			t.Run(route.name+"/"+scenario.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				route.handler(scenario.operation, scenario.err(route.invalid)).ServeHTTP(recorder, request)
				assertMaintenanceErrorEnvelope(t, recorder, scenario.status)

				var body map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode operation error: %v", err)
				}
				if scenario.wantFields {
					for field, want := range map[string]string{"operation_id": "operation_test", "status": "recovery_required", "apply_backup_id": "backup_apply"} {
						if body[field] != want {
							t.Errorf("%s = %#v, want %q; body=%s", field, body[field], want, recorder.Body.String())
						}
					}
				} else if _, ok := body["operation_id"]; ok {
					t.Fatalf("pre-operation failure fabricated operation metadata: %s", recorder.Body.String())
				}
				if strings.Contains(recorder.Body.String(), "private backup path") {
					t.Fatalf("internal error detail leaked: %s", recorder.Body.String())
				}
			})
		}
	}
}

func TestMaintenanceMethodErrorsReturnJSONAndAllow(t *testing.T) {
	routes := []struct {
		name, method, path, allow string
		handler                   http.Handler
	}{
		{"data health", http.MethodPost, DataHealthRoute, http.MethodGet, NewDataHealthHandler(nil)},
		{"operation history", http.MethodPost, maintenanceOperationsPath, http.MethodGet, NewMaintenanceOperationsHandler(nil)},
		{"trash list", http.MethodDelete, trashReadPath, http.MethodGet, NewTrashReadHandler(nil)},
		{"trash detail", http.MethodPut, trashReadPath + "/entry_1", http.MethodGet, NewTrashReadHandler(nil)},
		{"trash restore", http.MethodGet, trashReadPath + "/entry_1/restore", http.MethodPost, NewTrashReadHandler(nil)},
		{"trash restore preview", http.MethodGet, trashReadPath + "/entry_1/restore/dry-run", http.MethodPost, NewTrashReadHandler(nil)},
		{"backup create", http.MethodGet, BackupCreateRoute, http.MethodPost, NewBackupCreateHandler(nil)},
		{"backup detail", http.MethodPut, backupPath + "backup_1", http.MethodGet, NewBackupDetailHandler(nil)},
		{"backup restore", http.MethodGet, backupPath + "backup_1/restore/dry-run", http.MethodPost, NewBackupDetailHandler(nil)},
		{"cleanup plan", http.MethodGet, CleanupDryRunRoute, http.MethodPost, NewCleanupDryRunHandler(nil)},
		{"cleanup apply", http.MethodGet, CleanupApplyRoute, http.MethodPost, NewCleanupApplyHandler(nil)},
		{"migration plan", http.MethodGet, MigrationDryRunRoute, http.MethodPost, NewMigrationDryRunHandler(nil)},
		{"bedroom preset plan", http.MethodGet, BedroomPresetDryRunRoute, http.MethodPost, NewBedroomPresetDryRunHandler(nil)},
		{"migration apply", http.MethodGet, MigrationApplyRoute, http.MethodPost, NewMigrationApplyHandler(nil)},
		{"migration verify", http.MethodGet, MigrationVerifyRoute, http.MethodPost, NewMigrationVerifyHandler(nil)},
		{"rollback plan", http.MethodGet, MigrationRollbackDryRunRoute, http.MethodPost, NewMigrationRollbackDryRunHandler(nil)},
		{"rollback apply", http.MethodGet, MigrationRollbackApplyRoute, http.MethodPost, NewMigrationRollbackApplyHandler(nil)},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			route.handler.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
			assertMaintenanceErrorEnvelope(t, recorder, http.StatusMethodNotAllowed)
			if got := recorder.Header().Get("Allow"); got != route.allow {
				t.Fatalf("Allow = %q, want %q", got, route.allow)
			}
		})
	}
}

func TestMaintenanceReadOnlyHistoryFailuresUseBasicEnvelope(t *testing.T) {
	for _, test := range []struct {
		name   string
		path   string
		reader maintenanceHistoryReader
		status int
	}{
		{name: "unavailable list", path: maintenanceOperationsPath, status: http.StatusServiceUnavailable},
		{name: "corrupt list", path: maintenanceOperationsPath, reader: services.NewMaintenanceHistoryService(historyFixture{err: repositories.ErrMaintenanceCorrupt}), status: http.StatusServiceUnavailable},
		{name: "missing detail", path: maintenanceOperationsPath + "/missing", reader: services.NewMaintenanceHistoryService(historyFixture{}), status: http.StatusNotFound},
		{name: "invalid detail ID", path: maintenanceOperationsPath + "/bad!id", reader: services.NewMaintenanceHistoryService(historyFixture{err: repositories.ErrMaintenanceID}), status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &MaintenanceOperationsHandler{history: test.reader}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			assertMaintenanceErrorEnvelope(t, recorder, test.status)
		})
	}
}

func assertMaintenanceErrorEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, status, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if payload.OK || payload.Error == "" {
		t.Fatalf("error envelope = %#v", payload)
	}
}
