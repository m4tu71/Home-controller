package repositories

import (
	"encoding/json"
	"fmt"
	"os"
)

// Plans are opaque: ID issuance, payload schema and digest belong to callers.
type MaintenancePlanRepository struct{ state *MaintenanceState }

func (r *MaintenancePlanRepository) Create(id string, payload json.RawMessage) error {
	path, err := maintenancePath(r.state, "plans", id)
	if err != nil {
		return err
	}
	if !json.Valid(payload) {
		return fmt.Errorf("%w: invalid plan JSON", ErrMaintenanceCorrupt)
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Version < 1 {
		return fmt.Errorf("%w: plan version required", ErrMaintenanceCorrupt)
	}
	return r.state.withLock(func() error {
		_, err := os.Lstat(path)
		if err == nil {
			return ErrMaintenanceDuplicate
		}
		if !os.IsNotExist(err) {
			return err
		}
		return r.state.writer.WriteJSON(path, payload, 0600)
	})
}

func (r *MaintenancePlanRepository) Load(id string) (json.RawMessage, error) {
	path, err := maintenancePath(r.state, "plans", id)
	if err != nil {
		return nil, err
	}
	var payload json.RawMessage
	err = r.state.withLock(func() error {
		data, err := r.state.readFile(path)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return ErrMaintenanceCorrupt
		}
		var envelope struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.Version < 1 {
			return ErrMaintenanceCorrupt
		}
		payload = append(payload[:0], data...)
		return nil
	})
	return payload, err
}
