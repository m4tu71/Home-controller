package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"backend/models"
	"backend/repositories"
)

// BackupService accepts only Backend-resolved sources. Plan/action validation
// and the HTTP create route are deliberately deferred to Step 1I.
type BackupService struct {
	store interface {
		Create(repositories.BackupCreate) (models.BackupDetail, error)
		Load(string) (models.BackupDetail, error)
		RestoreDryRun(string) (repositories.BackupRestorePreview, error)
		RestoreRehearsal(string) (repositories.BackupRestoreResult, error)
	}
	operations interface {
		Load(string) (models.MaintenanceOperation, error)
		Update(models.MaintenanceOperation) error
		HasExclusivePlanClaim(string, string) error
	}
	plans *MaintenancePlanService
}

func NewBackupService(store *repositories.BackupRepository, operations interface {
	Load(string) (models.MaintenanceOperation, error)
	Update(models.MaintenanceOperation) error
	HasExclusivePlanClaim(string, string) error
}) *BackupService {
	if store != nil {
		store.WithRestoreReadbackValidator(validateBackupStatefulReadback)
	}
	return &BackupService{store: store, operations: operations}
}

func (s *BackupService) WithPlanService(plans *MaintenancePlanService) *BackupService {
	if s != nil {
		s.plans = plans
	}
	return s
}

func (s *BackupService) Detail(id string) (models.BackupDetail, error) {
	if s == nil || s.store == nil {
		return models.BackupDetail{}, errors.New("backup unavailable")
	}
	return s.store.Load(id)
}

func (s *BackupService) RestoreDryRun(id string) (repositories.BackupRestorePreview, error) {
	if s == nil || s.store == nil {
		return repositories.BackupRestorePreview{}, errors.New("backup restore unavailable")
	}
	return s.store.RestoreDryRun(id)
}

func (s *BackupService) RestoreRehearsal(id string) (repositories.BackupRestoreResult, error) {
	if s == nil || s.store == nil {
		return repositories.BackupRestoreResult{}, errors.New("backup restore unavailable")
	}
	return s.store.RestoreRehearsal(id)
}

func (s *BackupService) CreateRehearsal(request models.BackupCreateRequest) (models.BackupDetail, error) {
	if s == nil || s.store == nil || s.plans == nil {
		return models.BackupDetail{}, errors.New("backup plan verifier unavailable")
	}
	selection, err := s.plans.VerifyBackupSelection(request.PlanID, request.PlanDigest, request.ActionIDs)
	if err != nil {
		return models.BackupDetail{}, err
	}
	detail, err := s.create("rehearsal", request.PlanID, selection.ActionIDs, selection.Sources, selection.InventoryHash)
	if err != nil {
		return detail, err
	}
	if err := s.plans.VerifyRehearsalBackup(request.PlanID, request.PlanDigest, selection.ActionIDs, detail, false); err != nil {
		return detail, err
	}
	return detail, nil
}

// CreateApply is called inside an applying operation before its first data
// mutation; the returned ID is persisted to that operation before proceeding.
func (s *BackupService) CreateApply(operationID string) (models.BackupDetail, error) {
	if s == nil || s.operations == nil || s.store == nil || s.plans == nil {
		return models.BackupDetail{}, errors.New("operation state unavailable")
	}
	op, err := s.operations.Load(operationID)
	if err != nil {
		return models.BackupDetail{}, err
	}
	if op.ApplyBackupID != "" || op.Status != "applying" || op.PlanID == "" || op.PlanDigest == "" || op.Type == "" {
		return models.BackupDetail{}, repositories.ErrBackupConflict
	}
	if err := s.operations.HasExclusivePlanClaim(op.PlanID, op.ID); err != nil {
		return models.BackupDetail{}, err
	}
	actionIDs := make([]string, 0, len(op.Actions))
	for _, action := range op.Actions {
		actionIDs = append(actionIDs, action.ID)
	}
	selection, err := s.plans.VerifyApplySelection(op.PlanID, op.PlanDigest, actionIDs)
	if err != nil {
		return models.BackupDetail{}, err
	}
	if selection.Plan.OperationType != op.Type {
		return models.BackupDetail{}, ErrMaintenancePlanStale
	}
	if selection.Plan.BackupRequirement == "rehearsal_and_apply" {
		if op.RehearsalBackupID == "" {
			return models.BackupDetail{}, ErrMaintenancePlanNotReady
		}
		rehearsal, err := s.store.Load(op.RehearsalBackupID)
		if err != nil {
			return models.BackupDetail{}, err
		}
		if err := s.plans.VerifyRehearsalBackup(op.PlanID, op.PlanDigest, selection.ActionIDs, rehearsal, true); err != nil {
			return models.BackupDetail{}, err
		}
	}
	detail, err := s.create("apply", op.PlanID, selection.ActionIDs, selection.Sources, selection.InventoryHash)
	if err != nil {
		return detail, err
	}
	op.ApplyBackupID = detail.Bundle.ID
	op.Revision++
	if err := s.operations.Update(op); err != nil {
		return detail, err // The sealed bundle exists; preserve its ID for recovery.
	}
	if err := s.plans.VerifyApplyBackup(op.PlanID, op.PlanDigest, selection.ActionIDs, detail); err != nil {
		return detail, err
	}
	return detail, nil
}

func (s *BackupService) create(role, planID string, actionIDs, sources []string, expectedInventoryHash string) (models.BackupDetail, error) {
	if s == nil || s.store == nil {
		return models.BackupDetail{}, errors.New("backup unavailable")
	}
	id, err := newBackupID()
	if err != nil {
		return models.BackupDetail{}, err
	}
	return s.store.Create(repositories.BackupCreate{ID: id, Role: role, PlanID: planID, ActionIDs: actionIDs, Sources: sources, ExpectedInventoryHash: expectedInventoryHash})
}

func newBackupID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "backup_" + hex.EncodeToString(random[:]), nil
}
