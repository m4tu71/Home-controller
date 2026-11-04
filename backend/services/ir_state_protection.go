package services

import (
	"backend/models"
	"reflect"
)

func hasRegisteredStateSignal(state models.IRState) bool {
	switch state.Format {
	case models.OperationFormatRawSamples:
		return isLearnedRawSamplesState(state)
	case models.OperationFormatRaw:
		return state.Count != 0 || len(state.TimingsUS) != 0
	case models.OperationFormatCode:
		return state.Protocol != "" || state.Code != ""
	case models.OperationFormatPresetCommand:
		return state.PicoCommand != ""
	}
	return false
}

// Names and repeat settings can change. Values and registered signal content
// must keep the same meaning; users create and learn a new State to change it.
func validateRegisteredStateWrite(existing models.IRState, request models.IRStateWriteRequest) error {
	if !hasRegisteredStateSignal(existing) {
		return nil
	}
	existing = normalizeIRState(existing)
	changed := (request.State != nil && !reflect.DeepEqual(existing.State, *request.State)) ||
		(request.Format != nil && existing.Format != *request.Format) ||
		(request.CarrierKHz != nil && existing.CarrierKHz != *request.CarrierKHz) ||
		(request.Protocol != nil && existing.Protocol != *request.Protocol) ||
		(request.Code != nil && existing.Code != *request.Code) ||
		(request.PicoCommand != nil && existing.PicoCommand != *request.PicoCommand)
	// raw_samples learning fields retain their existing read-only contract.
	if existing.Format != models.OperationFormatRawSamples {
		changed = changed || (request.Count != nil && existing.Count != *request.Count) ||
			(request.TimingsUS != nil && !reflect.DeepEqual(existing.TimingsUS, *request.TimingsUS)) ||
			(request.Captures != nil && !reflect.DeepEqual(existing.Captures, *request.Captures))
	}
	if !changed {
		return nil
	}
	message := "registered ir state cannot be changed; create a new state"
	return &ServiceError{Kind: ErrorConflict, Message: message, Payload: map[string]interface{}{
		"ok": false, "error": message, "issues": []models.IRStateSchemaIssue{{FieldID: "state", Code: "learned_state_change", Message: message, StateID: existing.ID}},
	}}
}

func (s IRAdapterService) validateStoredStateForOperation(roomID, deviceID string, state models.IRState) error {
	schema, err := s.GetStateSchema(roomID, deviceID)
	if err != nil {
		return err
	}
	issues := irStateSchemaIssues(state.State, schema)
	if len(issues) == 0 {
		return nil
	}
	for i := range issues {
		issues[i].StateID = state.ID
	}
	return &ServiceError{Kind: ErrorConflict, Message: irStateDoesNotMatchSchemaErrorMessage, Payload: map[string]interface{}{"ok": false, "error": irStateDoesNotMatchSchemaErrorMessage, "issues": issues}}
}
