package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
)

type bedroomPresetPlannerStub struct{}

func (bedroomPresetPlannerStub) Create() (models.MaintenancePlanEnvelope, error) {
	return models.MaintenancePlanEnvelope{PlanID: "plan_test"}, nil
}

func TestBedroomPresetDryRunHTTPBoundary(t *testing.T) {
	h := NewBedroomPresetDryRunHandler(bedroomPresetPlannerStub{})
	for _, tc := range []struct {
		name, method, body string
		status             int
	}{
		{"empty", http.MethodPost, "", 201},
		{"json_body_rejected", http.MethodPost, "{}", 400},
		{"oversized", http.MethodPost, strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), 413},
		{"method", http.MethodGet, "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, BedroomPresetDryRunRoute, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
