package handlers

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/services"
)

type legacyIRPlannerStub struct {
	calls int
	plan  models.MaintenancePlanEnvelope
	err   error
}

func (s *legacyIRPlannerStub) Create(models.LegacyIRCutoverDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	s.calls++
	return s.plan, s.err
}

type legacyIRApplierStub struct {
	calls int
	op    models.MaintenanceOperation
	err   error
}

func (s *legacyIRApplierStub) Apply(models.LegacyIRCutoverApplyRequest) (models.MaintenanceOperation, error) {
	s.calls++
	return s.op, s.err
}

func TestLegacyIRCutoverDryRunStrictInput(t *testing.T) {
	body := `{"migration_operation_id":"migration_test","sources":[{"path":"ir-signals/one.json","classification":"intentionally_skipped"}]}`
	for _, tc := range []struct {
		name, method, path, contentType, body string
		want                                  int
	}{
		{"method", "GET", LegacyIRCutoverDryRunRoute, "application/json", body, 405},
		{"query", "POST", LegacyIRCutoverDryRunRoute + "?all=true", "application/json", body, 400},
		{"content_type", "POST", LegacyIRCutoverDryRunRoute, "", body, 415},
		{"unknown", "POST", LegacyIRCutoverDryRunRoute, "application/json", strings.TrimSuffix(body, "}") + `,"force":true}`, 400},
		{"trailing", "POST", LegacyIRCutoverDryRunRoute, "application/json", body + `{}`, 400},
		{"empty", "POST", LegacyIRCutoverDryRunRoute, "application/json", `{"migration_operation_id":"x","sources":[]}`, 400},
		{"unavailable", "POST", LegacyIRCutoverDryRunRoute, "application/json", body, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			NewLegacyIRCutoverDryRunHandler(nil).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	stub := &legacyIRPlannerStub{plan: models.MaintenancePlanEnvelope{PlanID: "plan_test", Digest: "sha256:test", ReadyToApply: true, BackupRequirement: "rehearsal_and_apply", Actions: []models.MaintenancePlanAction{{ID: "archive_legacy_ir_signals", BackupFileCount: 1}}}}
	r := httptest.NewRequest("POST", LegacyIRCutoverDryRunRoute, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NewLegacyIRCutoverDryRunHandler(stub).ServeHTTP(w, r)
	if w.Code != 201 || stub.calls != 1 || !strings.Contains(w.Body.String(), `"source_path":"data/ir-signals"`) {
		t.Fatalf("response=%d calls=%d %s", w.Code, stub.calls, w.Body.String())
	}
}

func TestLegacyIRCutoverApplyRequiresExactPathAndHidesErrors(t *testing.T) {
	body := `{"plan_id":"plan_test","plan_digest":"sha256:test","rehearsal_backup_id":"backup_test","confirm_source_path":"data/ir-signals","confirm":true}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"missing_confirm_path", strings.Replace(body, `"data/ir-signals"`, `""`, 1), 400},
		{"wrong_confirm_path", strings.Replace(body, `"data/ir-signals"`, `"data/rooms"`, 1), 400},
		{"no_confirm", strings.Replace(body, `"confirm":true`, `"confirm":false`, 1), 400},
		{"unknown", strings.TrimSuffix(body, "}") + `,"force":true}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", LegacyIRCutoverApplyRoute, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			NewLegacyIRCutoverApplyHandler(&legacyIRApplierStub{}).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	stub := &legacyIRApplierStub{err: errors.Join(services.ErrLegacyIRCutoverStale, errors.New("private filesystem path"))}
	r := httptest.NewRequest("POST", LegacyIRCutoverApplyRoute, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NewLegacyIRCutoverApplyHandler(stub).ServeHTTP(w, r)
	if w.Code != 409 || stub.calls != 1 || strings.Contains(w.Body.String(), "private filesystem path") {
		t.Fatalf("response=%d calls=%d %s", w.Code, stub.calls, w.Body.String())
	}
	success := &legacyIRApplierStub{op: models.MaintenanceOperation{ID: "legacy_cutover_test", Status: "verified", Result: "verified", Actions: []models.MaintenanceOperationAction{{ID: "archive_legacy_ir_signals", IntendedState: []byte(`{"trash":{"path":"entry_1/payload"}}`)}}}}
	r = httptest.NewRequest("POST", LegacyIRCutoverApplyRoute, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	NewLegacyIRCutoverApplyHandler(success).ServeHTTP(w, r)
	if w.Code != 200 || success.calls != 1 || !strings.Contains(w.Body.String(), `"operation_id":"legacy_cutover_test"`) || !strings.Contains(w.Body.String(), `"trash_entry_id":"entry_1"`) {
		t.Fatalf("success response=%d calls=%d %s", w.Code, success.calls, w.Body.String())
	}
}
