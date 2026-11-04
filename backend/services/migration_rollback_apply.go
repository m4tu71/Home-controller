package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"backend/models"
	"backend/repositories"
)

// MigrationRollbackApplyService moves only proven, unchanged migration targets.
// A durable target claim prevents another rollback plan from racing this one.
type MigrationRollbackApplyService struct {
	plans      *MigrationRollbackPlanService
	operations cleanupOperationStore
	backup     cleanupApplyBackup
	trash      cleanupTrashStore
	inspector  MaintenanceFilesystemInspector
	newID      func(string) (string, error)
}

func NewMigrationRollbackApplyService(plans *MigrationRollbackPlanService, operations *repositories.MaintenanceOperationRepository, backup *BackupService, trash *repositories.TrashRepository, runtimeRoot, trashRoot string) *MigrationRollbackApplyService {
	return &MigrationRollbackApplyService{plans: plans, operations: operations, backup: backup, trash: trash, inspector: MaintenanceFilesystemInspector{RuntimeRoot: runtimeRoot, TrashRoot: trashRoot}, newID: newCleanupID}
}

type rollbackSelected struct {
	target   models.MigrationRollbackTarget
	entryID  string
	before   models.MaintenanceSnapshot
	intended models.MaintenanceSnapshot
}

func (s *MigrationRollbackApplyService) Apply(request models.MigrationRollbackApplyRequest) (models.MaintenanceOperation, error) {
	if s == nil || s.plans == nil || s.plans.plans == nil || s.plans.migration == nil || s.plans.operations == nil || s.plans.backups == nil || s.operations == nil || s.backup == nil || s.trash == nil || s.newID == nil || !filepath.IsAbs(s.inspector.RuntimeRoot) || !filepath.IsAbs(s.inspector.TrashRoot) || s.plans.inspector.RuntimeRoot != s.inspector.RuntimeRoot {
		return models.MaintenanceOperation{}, ErrMaintenancePlanNotReady
	}
	if err := request.Validate(); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrMigrationRollbackInvalid, err)
	}
	plan, err := s.plans.Load(request.PlanID)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if plan.Digest != request.PlanDigest {
		return models.MaintenanceOperation{}, ErrMaintenancePlanStale
	}
	claims, err := s.operations.List()
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	for _, claim := range claims {
		if claim.PlanID == plan.PlanID {
			return models.MaintenanceOperation{}, repositories.ErrMaintenancePlanClaimed
		}
	}
	if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, request.ActionIDs); err != nil {
		return models.MaintenanceOperation{}, err
	}
	var payload models.MigrationRollbackPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrMigrationRollbackInvalid, err)
	}
	byID := map[string]models.MigrationRollbackTarget{}
	for _, target := range payload.Targets {
		byID[target.ActionID] = target
	}
	selected := make([]models.MigrationRollbackTarget, 0, len(request.ActionIDs))
	for _, id := range request.ActionIDs {
		target, ok := byID[id]
		if !ok {
			return models.MaintenanceOperation{}, ErrMaintenancePlanActionMissing
		}
		if !target.Eligible {
			return models.MaintenanceOperation{}, ErrMaintenancePlanNotReady
		}
		selected = append(selected, target)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ActionID < selected[j].ActionID })
	if err := s.revalidate(payload, selected); err != nil {
		return models.MaintenanceOperation{}, err
	}
	opID, err := s.newID("rollback_")
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	prepared := make([]rollbackSelected, 0, len(selected))
	paths := make([]string, 0, len(selected))
	for _, target := range selected {
		entryID, err := s.newID("trash_")
		if err != nil {
			return models.MaintenanceOperation{}, err
		}
		runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, target.TargetPath)
		if err != nil || !runtime.Exists || runtime.Kind != "file" || runtime.SHA256 != target.ApplySHA256 {
			return models.MaintenanceOperation{}, errors.Join(ErrMaintenancePlanStale, err)
		}
		trashBefore, err := s.inspector.observe(s.inspector.TrashRoot, entryID+"/payload")
		if err != nil || trashBefore.Exists {
			return models.MaintenanceOperation{}, errors.Join(ErrMaintenancePlanStale, err)
		}
		trashAfter := runtime
		trashAfter.Path = entryID + "/payload"
		runtimeAfter := runtime
		runtimeAfter.Exists = false
		runtimeAfter.Kind = ""
		runtimeAfter.SHA256 = ""
		prepared = append(prepared, rollbackSelected{target: target, entryID: entryID, before: models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trashBefore}, intended: models.MaintenanceSnapshot{Runtime: &runtimeAfter, Trash: &trashAfter}})
		paths = append(paths, target.TargetPath)
	}
	work := make([]MaintenanceActionWork, 0, len(prepared))
	for _, preparedAction := range prepared {
		a := preparedAction
		kind, _, _, targetID, ok := migrationTarget(a.target.TargetPath)
		if !ok {
			return models.MaintenanceOperation{}, ErrMigrationRollbackInvalid
		}
		before, _ := json.Marshal(a.before)
		intended, _ := json.Marshal(a.intended)
		work = append(work, MaintenanceActionWork{ID: a.target.ActionID, Action: "rollback_target", BeforeState: before, IntendedState: intended,
			Mutate: func() error {
				current, err := s.inspector.observe(s.inspector.RuntimeRoot, a.target.TargetPath)
				if err != nil || current != *a.before.Runtime {
					return errors.Join(ErrMaintenancePlanStale, err)
				}
				trash, err := s.inspector.observe(s.inspector.TrashRoot, a.entryID+"/payload")
				if err != nil || trash.Exists {
					return errors.Join(ErrMaintenancePlanStale, err)
				}
				_, err = s.trash.Move(repositories.TrashMoveRequest{EntryID: a.entryID, SourceRelativePath: a.target.TargetPath, OriginalID: targetID, TargetType: "ir_" + kind, Reason: "migration_rollback:" + payload.MigrationOperationID, OperationType: "migration_rollback", OperationID: opID})
				return err
			},
			Readback: func() (json.RawMessage, error) {
				entry, err := s.trash.Load(a.entryID)
				if err != nil {
					return nil, err
				}
				if entry.Status != "sealed" || !entry.RestoreAvailable || entry.OperationID != opID || entry.OriginalRelativePath != a.target.TargetPath || entry.TargetType != "ir_"+kind {
					return nil, fmt.Errorf("rollback Trash readback mismatch")
				}
				runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, a.target.TargetPath)
				if err != nil {
					return nil, err
				}
				trash, err := s.inspector.observe(s.inspector.TrashRoot, a.entryID+"/payload")
				if err != nil {
					return nil, err
				}
				actual := models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trash}
				if !sameSnapshot(actual, a.intended) {
					return nil, fmt.Errorf("rollback target readback mismatch")
				}
				return json.Marshal(actual)
			},
		})
	}
	draft := models.MaintenanceOperation{ID: opID, Target: "migration_rollback", Type: "migration_rollback", PlanID: plan.PlanID, PlanDigest: plan.Digest, ClaimedPaths: paths}
	return NewMaintenanceCoordinator(s.operations).RunWithBeforeMutation(draft, work, func(id string) (models.MaintenanceOperation, error) {
		_, backupErr := s.backup.CreateApply(id)
		updated, loadErr := s.operations.Load(id)
		if loadErr != nil {
			return updated, errors.Join(backupErr, loadErr)
		}
		if backupErr != nil {
			return updated, backupErr
		}
		if updated.ApplyBackupID == "" {
			return updated, ErrMaintenancePlanNotReady
		}
		if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, request.ActionIDs); err != nil {
			return updated, err
		}
		if err := s.revalidate(payload, selected); err != nil {
			return updated, err
		}
		return updated, nil
	})
}

func (s *MigrationRollbackApplyService) revalidate(payload models.MigrationRollbackPlanPayload, selected []models.MigrationRollbackTarget) error {
	op, err := s.plans.operations.Load(payload.MigrationOperationID)
	if err != nil {
		return err
	}
	if op.Type != "migration" || op.PlanID != payload.MigrationPlanID || op.PlanDigest != payload.MigrationPlanDigest || op.ApplyBackupID == "" || op.ApplyBackupID != payload.ApplyBackupID {
		return ErrMigrationRollbackInvalid
	}
	backup, err := s.plans.backups.Detail(op.ApplyBackupID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(op.Actions))
	for _, action := range op.Actions {
		ids = append(ids, action.ID)
	}
	if backup.Registry.Role != "apply" || backup.Registry.State != "point_in_time_verified" || backup.Bundle.PlanID != op.PlanID || !sameStringSet(backup.Bundle.ActionIDs, ids) {
		return ErrMigrationRollbackInvalid
	}
	migrationPlan, err := s.plans.migration.Load(op.PlanID)
	if err != nil {
		return err
	}
	if migrationPlan.Digest != op.PlanDigest {
		return ErrMigrationRollbackInvalid
	}
	var source models.MigrationPlanPayload
	if json.Unmarshal(migrationPlan.Payload, &source) != nil {
		return ErrMigrationRollbackInvalid
	}
	items := map[string]models.MigrationPlanAction{}
	for _, item := range source.Actions {
		items[item.ActionID] = item
	}
	actions := map[string]models.MaintenanceOperationAction{}
	for _, action := range op.Actions {
		actions[action.ID] = action
	}
	for _, target := range selected {
		item, okItem := items[target.ActionID]
		action, okAction := actions[target.ActionID]
		if !okItem || !okAction || item.Action != action.Action {
			return ErrMigrationRollbackInvalid
		}
		current := s.plans.candidate(op, item, action, nil)
		if !current.Eligible || current.TargetPath != target.TargetPath || current.ApplySHA256 != target.ApplySHA256 || current.CurrentSHA256 != target.CurrentSHA256 {
			return ErrMaintenancePlanStale
		}
	}
	return nil
}
