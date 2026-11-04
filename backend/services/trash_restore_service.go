package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"backend/models"
	"backend/repositories"
)

type TrashRestoreStore interface {
	PreviewRestore(string) (repositories.TrashRestorePreview, error)
	Restore(string, string, string) (models.TrashEntry, error)
}

type TrashRestoreService struct {
	store       TrashRestoreStore
	coordinator *MaintenanceCoordinator
	inspector   MaintenanceFilesystemInspector
}

func NewTrashRestoreService(store TrashRestoreStore, operations MaintenanceOperationStore, runtimeRoot, trashRoot string) *TrashRestoreService {
	return &TrashRestoreService{store: store, coordinator: NewMaintenanceCoordinator(operations), inspector: MaintenanceFilesystemInspector{RuntimeRoot: runtimeRoot, TrashRoot: trashRoot}}
}

func (s *TrashRestoreService) DryRun(id string) (repositories.TrashRestorePreview, error) {
	if s == nil || s.store == nil {
		return repositories.TrashRestorePreview{}, errors.New("Trash restore unavailable")
	}
	return s.store.PreviewRestore(id)
}

func (s *TrashRestoreService) Restore(id, hash string) (models.TrashEntry, string, error) {
	if s == nil || s.store == nil || s.coordinator == nil {
		return models.TrashEntry{}, "", errors.New("Trash restore unavailable")
	}
	preview, err := s.store.PreviewRestore(id)
	if err != nil {
		return models.TrashEntry{}, "", err
	}
	if !preview.RestoreAvailable || preview.ExpectedInventoryHash != hash {
		return models.TrashEntry{}, "", repositories.ErrTrashChanged
	}
	operationID, err := newTrashRestoreOperationID()
	if err != nil {
		return models.TrashEntry{}, "", err
	}
	beforeRuntime, err := s.inspector.observe(s.inspector.RuntimeRoot, preview.OriginalRelativePath)
	if err != nil {
		return models.TrashEntry{}, "", err
	}
	beforeTrash, err := s.inspector.observe(s.inspector.TrashRoot, id+"/payload")
	if err != nil {
		return models.TrashEntry{}, "", err
	}
	if beforeRuntime.Exists || !beforeTrash.Exists {
		return models.TrashEntry{}, "", repositories.ErrTrashChanged
	}
	intendedRuntime := beforeTrash
	intendedRuntime.Path = preview.OriginalRelativePath
	intendedTrash := beforeRuntime
	intendedTrash.Path = id + "/payload"
	before, _ := json.Marshal(models.MaintenanceSnapshot{Runtime: &beforeRuntime, Trash: &beforeTrash})
	intended, _ := json.Marshal(models.MaintenanceSnapshot{Runtime: &intendedRuntime, Trash: &intendedTrash})
	var restored models.TrashEntry
	_, err = s.coordinator.Run(models.MaintenanceOperation{ID: operationID, Target: id, Type: "trash_restore"}, []MaintenanceActionWork{{
		ID: "restore", Action: "trash_restore", BeforeState: before, IntendedState: intended,
		Mutate: func() error { var e error; restored, e = s.store.Restore(id, hash, operationID); return e },
		Readback: func() (json.RawMessage, error) {
			runtime, e := s.inspector.observe(s.inspector.RuntimeRoot, preview.OriginalRelativePath)
			if e != nil {
				return nil, e
			}
			trash, e := s.inspector.observe(s.inspector.TrashRoot, id+"/payload")
			if e != nil {
				return nil, e
			}
			actual := models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trash}
			if !sameSnapshot(actual, models.MaintenanceSnapshot{Runtime: &intendedRuntime, Trash: &intendedTrash}) {
				return nil, errors.New("restore readback mismatch")
			}
			return json.Marshal(actual)
		},
	}})
	if err != nil {
		return models.TrashEntry{}, operationID, err
	}
	return restored, operationID, nil
}

func newTrashRestoreOperationID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("operation ID: %w", err)
	}
	return "restore_" + hex.EncodeToString(random[:]), nil
}
