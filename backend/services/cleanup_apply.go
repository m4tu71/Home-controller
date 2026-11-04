package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"backend/models"
	"backend/repositories"
)

type cleanupOperationStore interface {
	MaintenanceOperationStore
	Load(string) (models.MaintenanceOperation, error)
	List() ([]models.MaintenanceOperation, error)
}

type cleanupApplyBackup interface {
	CreateApply(string) (models.BackupDetail, error)
}

type cleanupTrashStore interface {
	Move(repositories.TrashMoveRequest) (repositories.TrashMoveResult, error)
	Load(string) (models.TrashEntry, error)
}

type CleanupApplyService struct {
	plans      *CleanupPlanService
	operations cleanupOperationStore
	backup     cleanupApplyBackup
	trash      cleanupTrashStore
	inspector  MaintenanceFilesystemInspector
	newID      func(string) (string, error)
}

func NewCleanupApplyService(plans *CleanupPlanService, operations *repositories.MaintenanceOperationRepository, backup *BackupService, trash *repositories.TrashRepository, runtimeRoot, trashRoot string) *CleanupApplyService {
	return &CleanupApplyService{plans: plans, operations: operations, backup: backup, trash: trash, inspector: MaintenanceFilesystemInspector{RuntimeRoot: runtimeRoot, TrashRoot: trashRoot}, newID: newCleanupID}
}

func newCleanupID(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random[:]), nil
}

type cleanupSelectedAction struct {
	item     models.CleanupPlanAction
	before   models.MaintenanceSnapshot
	intended models.MaintenanceSnapshot
	entryID  string
}

// Apply fails closed before the first mutation if any selected issue or
// backup source changed. Once claimed, failures are durable and never retried
// automatically; callers inspect the operation and Trash for recovery.
func (s *CleanupApplyService) Apply(request models.CleanupApplyRequest) (models.MaintenanceOperation, error) {
	if s == nil || s.plans == nil || s.plans.plans == nil || s.plans.health == nil || s.operations == nil || s.backup == nil || s.trash == nil || s.newID == nil || s.inspector.RuntimeRoot == "" || s.inspector.TrashRoot == "" {
		return models.MaintenanceOperation{}, ErrMaintenancePlanNotReady
	}
	if err := request.Validate(); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrCleanupSelectionInvalid, err)
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
	if s.plans.health.Repository.Root != s.inspector.RuntimeRoot {
		return models.MaintenanceOperation{}, ErrMaintenancePlanInvalid
	}
	var payload models.CleanupPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrMaintenancePlanInvalid, err)
	}
	byID := map[string]models.CleanupPlanAction{}
	for _, item := range payload.Actions {
		byID[item.ActionID] = item
	}
	selection := map[string]string{}
	for _, item := range request.Issues {
		selection[item.IssueID] = item.BeforeHash
	}
	if len(selection) != len(request.ActionIDs) {
		return models.MaintenanceOperation{}, ErrCleanupSelectionInvalid
	}
	selected := make([]models.CleanupPlanAction, 0, len(request.ActionIDs))
	for _, id := range request.ActionIDs {
		item, ok := byID[id]
		if !ok {
			return models.MaintenanceOperation{}, ErrMaintenancePlanActionMissing
		}
		if selection[item.IssueID] != item.BeforeHash {
			return models.MaintenanceOperation{}, ErrCleanupSelectionInvalid
		}
		selected = append(selected, item)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ActionID < selected[j].ActionID })
	if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, request.ActionIDs); err != nil {
		return models.MaintenanceOperation{}, err
	}
	if err := s.revalidateIssues(selected); err != nil {
		return models.MaintenanceOperation{}, err
	}
	opID, err := s.newID("cleanup_")
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	prepared := make([]cleanupSelectedAction, 0, len(selected))
	for _, item := range selected {
		entryID, err := s.newID("trash_")
		if err != nil {
			return models.MaintenanceOperation{}, err
		}
		rel := item.AffectedPaths[0]
		runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, rel)
		if err != nil || !runtime.Exists {
			return models.MaintenanceOperation{}, errors.Join(ErrCleanupIssueStale, err)
		}
		if (item.Action == "trash_file" && runtime.Kind != "file") || (item.Action == "trash_directory" && runtime.Kind != "directory") {
			return models.MaintenanceOperation{}, ErrCleanupIssueStale
		}
		trashRel := entryID + "/payload"
		trashBefore, err := s.inspector.observe(s.inspector.TrashRoot, trashRel)
		if err != nil || trashBefore.Exists {
			return models.MaintenanceOperation{}, errors.Join(ErrCleanupIssueStale, err)
		}
		trashAfter := runtime
		trashAfter.Path = trashRel
		runtimeAfter := runtime
		runtimeAfter.Exists = false
		runtimeAfter.Kind = ""
		runtimeAfter.SHA256 = ""
		prepared = append(prepared, cleanupSelectedAction{item: item, entryID: entryID, before: models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trashBefore}, intended: models.MaintenanceSnapshot{Runtime: &runtimeAfter, Trash: &trashAfter}})
	}
	work := make([]MaintenanceActionWork, 0, len(prepared))
	for _, action := range prepared {
		a := action
		before, _ := json.Marshal(a.before)
		intended, _ := json.Marshal(a.intended)
		work = append(work, MaintenanceActionWork{ID: a.item.ActionID, Action: a.item.Action, BeforeState: before, IntendedState: intended,
			Mutate: func() error {
				current, err := s.inspector.observe(s.inspector.RuntimeRoot, a.item.AffectedPaths[0])
				if err != nil || current != *a.before.Runtime {
					return errors.Join(ErrCleanupIssueStale, err)
				}
				currentTrash, err := s.inspector.observe(s.inspector.TrashRoot, a.entryID+"/payload")
				if err != nil || currentTrash.Exists {
					return errors.Join(ErrCleanupIssueStale, err)
				}
				_, err = s.trash.Move(repositories.TrashMoveRequest{EntryID: a.entryID, SourceRelativePath: a.item.AffectedPaths[0], OriginalID: filepath.Base(a.item.AffectedPaths[0]), TargetType: strings.TrimPrefix(a.item.Action, "trash_"), Reason: a.item.IssueID, OperationType: "cleanup", OperationID: opID})
				return err
			},
			Readback: func() (json.RawMessage, error) {
				entry, err := s.trash.Load(a.entryID)
				if err != nil {
					return nil, err
				}
				if entry.Status != "sealed" || !entry.RestoreAvailable || entry.OperationID != opID || entry.OriginalRelativePath != a.item.AffectedPaths[0] {
					return nil, fmt.Errorf("Trash entry readback mismatch")
				}
				runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, a.item.AffectedPaths[0])
				if err != nil {
					return nil, err
				}
				trash, err := s.inspector.observe(s.inspector.TrashRoot, a.entryID+"/payload")
				if err != nil {
					return nil, err
				}
				actual := models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trash}
				if !sameSnapshot(actual, a.intended) {
					return nil, fmt.Errorf("cleanup readback mismatch")
				}
				return json.Marshal(actual)
			},
		})
	}
	coordinator := NewMaintenanceCoordinator(s.operations)
	draft := models.MaintenanceOperation{ID: opID, Target: "cleanup", Type: "cleanup", PlanID: plan.PlanID, PlanDigest: plan.Digest}
	return coordinator.RunWithBeforeMutation(draft, work, func(id string) (models.MaintenanceOperation, error) {
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
		if err := s.revalidateIssues(selected); err != nil {
			return updated, err
		}
		return updated, nil
	})
}

func (s *CleanupApplyService) revalidateIssues(selected []models.CleanupPlanAction) error {
	issues, err := s.plans.health.Scan()
	if err != nil {
		return err
	}
	byID := make(map[string]models.DataHealthIssue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	for _, item := range selected {
		issue, ok := byID[item.IssueID]
		if !ok || issue.Hash != item.BeforeHash {
			return ErrCleanupIssueStale
		}
		action, rel, err := cleanupActionForIssue(s.inspector.RuntimeRoot, issue)
		if err != nil || rel != item.AffectedPaths[0] || action.Action != item.Action || action.ActionID != item.ActionID {
			return errors.Join(ErrCleanupIssueStale, err)
		}
	}
	return nil
}
