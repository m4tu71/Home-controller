package handlers

import (
	"errors"
	"net/http"
	"reflect"

	"backend/models"
	"backend/services"
)

const BedroomPresetDryRunRoute = "/api/controller/maintenance/bedroom-preset/dry-run"

type bedroomPresetPlanner interface {
	Create() (models.MaintenancePlanEnvelope, error)
}
type BedroomPresetDryRunHandler struct{ service bedroomPresetPlanner }

func NewBedroomPresetDryRunHandler(service bedroomPresetPlanner) *BedroomPresetDryRunHandler {
	if service != nil {
		v := reflect.ValueOf(service)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			service = nil
		}
	}
	return &BedroomPresetDryRunHandler{service: service}
}

func (h *BedroomPresetDryRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != BedroomPresetDryRunRoute {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	if r.URL.RawQuery != "" {
		writeMaintenanceInputError(w, 400, "query parameters are not supported")
		return
	}
	if !RequireEmptyMaintenanceBody(w, r, MaintenanceJSONBodyLimit256KiB) {
		return
	}
	if h == nil || h.service == nil {
		writeMaintenanceInputError(w, 503, "bedroom preset planning unavailable")
		return
	}
	plan, err := h.service.Create()
	if err != nil {
		if errors.Is(err, services.ErrBedroomPresetUnavailable) || errors.Is(err, services.ErrMaintenancePlanStale) {
			writeMaintenanceInputError(w, 409, "bedroom preset destination or backup anchor changed; inspect runtime data")
		} else {
			writeMaintenanceInputError(w, 503, "bedroom preset planning unavailable; inspect maintenance state")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeMaintenanceJSON(w, 201, map[string]any{"ok": true, "plan": plan})
}
