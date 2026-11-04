package services

import (
	"errors"
	"time"

	"backend/models"
)

type MaintenanceHistoryStore interface {
	List() ([]models.MaintenanceOperation, error)
	Load(string) (models.MaintenanceOperation, error)
	Update(models.MaintenanceOperation) error
}

type MaintenanceReconciler struct {
	store     MaintenanceHistoryStore
	inspector MaintenanceFilesystemInspector
	now       func() time.Time
}

func NewMaintenanceReconciler(store MaintenanceHistoryStore, inspector MaintenanceFilesystemInspector) *MaintenanceReconciler {
	return &MaintenanceReconciler{store: store, inspector: inspector, now: func() time.Time { return time.Now().UTC() }}
}

// Scan is startup-only. It never executes a business action or changes runtime
// or Trash data; its sole write marks review-required operation metadata.
func (r *MaintenanceReconciler) Scan() error {
	if r == nil || r.store == nil || r.now == nil {
		return errors.New("maintenance reconciler is not configured")
	}
	records, err := r.store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if !needsMaintenanceReconcile(record.Status) {
			continue
		}
		result := &models.MaintenanceRecovery{PreviousStatus: record.Status, CheckedAt: r.now(), Actions: make([]models.MaintenanceRecoveryAction, 0, len(record.Actions))}
		for _, action := range record.Actions {
			if record.Type == "migration" {
				// A migration action can create several scaffold paths before its
				// target. The generic single-path snapshot cannot prove completion.
				result.Actions = append(result.Actions, models.MaintenanceRecoveryAction{ActionID: action.ID, Classification: MaintenanceAmbiguous, Reason: "migration_multi_path_manual_review"})
			} else {
				result.Actions = append(result.Actions, r.inspector.Inspect(action))
			}
		}
		record.Recovery = result
		record.Status = "recovery_required"
		record.Revision++
		if err := r.store.Update(record); err != nil {
			return err
		}
	}
	return nil
}

func needsMaintenanceReconcile(status string) bool {
	switch status {
	case string(MaintenanceVerified), "recovery_required":
		return false
	default:
		return true
	}
}

type MaintenanceHistoryService struct{ store MaintenanceHistoryStore }

func NewMaintenanceHistoryService(store MaintenanceHistoryStore) *MaintenanceHistoryService {
	return &MaintenanceHistoryService{store: store}
}

func (s *MaintenanceHistoryService) List() ([]models.MaintenanceOperation, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("maintenance history unavailable")
	}
	return s.store.List()
}

func (s *MaintenanceHistoryService) Detail(id string) (models.MaintenanceOperation, error) {
	if s == nil || s.store == nil {
		return models.MaintenanceOperation{}, errors.New("maintenance history unavailable")
	}
	return s.store.Load(id)
}
