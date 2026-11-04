package services

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"backend/models"
	"backend/repositories"
)

var ErrTrashFinalDeleteConfirmation = errors.New("Trash final-delete entry ID confirmation mismatch")
var ErrTrashFinalDeleteAssociation = errors.New("Trash final-delete backup or operation association invalid")

type TrashFinalDeletePlanPayload struct {
	Snapshot          repositories.TrashFinalDeleteSnapshot `json:"snapshot"`
	ApplyBackupID     string                                `json:"apply_backup_id,omitempty"`
	RehearsalBackupID string                                `json:"rehearsal_backup_id,omitempty"`
}

type TrashFinalDeletePreview struct {
	EntryID           string                `json:"entry_id"`
	DeletedAt         time.Time             `json:"deleted_at"`
	EligibleAfter     time.Time             `json:"eligible_after"`
	CurrentTime       time.Time             `json:"current_time"`
	Eligible          bool                  `json:"eligible"`
	Inventory         models.TrashInventory `json:"inventory"`
	PlanID            string                `json:"plan_id,omitempty"`
	PlanDigest        string                `json:"plan_digest,omitempty"`
	OriginalOperation string                `json:"original_operation_id"`
	ApplyBackupID     string                `json:"apply_backup_id,omitempty"`
	RehearsalBackupID string                `json:"rehearsal_backup_id,omitempty"`
}

type trashFinalDeleteStore interface {
	PreviewFinalDelete(string) (repositories.TrashFinalDeleteSnapshot, error)
	DeleteFinal(repositories.TrashFinalDeleteSnapshot, string, string, func(int64) error) (repositories.TrashFinalDeleteResult, error)
	Load(string) (models.TrashEntry, error)
}

type trashFinalDeleteOperationStore interface {
	List() ([]models.MaintenanceOperation, error)
	Load(string) (models.MaintenanceOperation, error)
	Create(models.MaintenanceOperation) error
	Update(models.MaintenanceOperation) error
	HasExclusivePlanClaim(string, string) error
}

type TrashFinalDeleteService struct {
	trash   trashFinalDeleteStore
	plans   *MaintenancePlanService
	ops     trashFinalDeleteOperationStore
	backups interface {
		Load(string) (models.BackupDetail, error)
	}
	now       func() time.Time
	newID     func() (string, error)
	inspector MaintenanceFilesystemInspector
}

func NewTrashFinalDeleteService(trash *repositories.TrashRepository, plans *MaintenancePlanService, operations *repositories.MaintenanceOperationRepository, backups *repositories.BackupRepository, trashRoot string) *TrashFinalDeleteService {
	return &TrashFinalDeleteService{trash: trash, plans: plans, ops: operations, backups: backups, now: func() time.Time { return time.Now().UTC() }, newID: newTrashFinalDeleteOperationID, inspector: MaintenanceFilesystemInspector{TrashRoot: trashRoot}}
}

func (s *TrashFinalDeleteService) DryRun(entryID string) (TrashFinalDeletePreview, error) {
	if s == nil || s.trash == nil || s.plans == nil || s.ops == nil || s.backups == nil {
		return TrashFinalDeletePreview{}, ErrTrashFinalDeleteAssociation
	}
	snapshot, err := s.trash.PreviewFinalDelete(entryID)
	if err != nil {
		return TrashFinalDeletePreview{}, err
	}
	entry := snapshot.Entry
	preview := TrashFinalDeletePreview{EntryID: entry.ID, DeletedAt: *entry.DeletedAt, EligibleAfter: *entry.EligibleAfter, CurrentTime: snapshot.CurrentTime, Eligible: !snapshot.CurrentTime.Before(*entry.EligibleAfter), Inventory: entry.BeforeInventory, OriginalOperation: entry.OperationID}
	if !preview.Eligible {
		return preview, nil
	}
	op, err := s.verifyOriginalAssociation(entry)
	if err != nil {
		return preview, err
	}
	preview.ApplyBackupID, preview.RehearsalBackupID = op.ApplyBackupID, op.RehearsalBackupID
	payload, err := json.Marshal(TrashFinalDeletePlanPayload{Snapshot: snapshot, ApplyBackupID: op.ApplyBackupID, RehearsalBackupID: op.RehearsalBackupID})
	if err != nil {
		return preview, err
	}
	plan, err := s.plans.Create(models.MaintenancePlanEnvelope{OperationType: "trash_final_delete", PayloadSchemaID: "trash_final_delete.v1", Payload: payload, ReadyToApply: true, BackupRequirement: "none", Actions: []models.MaintenancePlanAction{{ID: "entry", Action: "trash_final_delete", Selectable: true, Destructive: true}}})
	if err != nil {
		return preview, err
	}
	preview.PlanID, preview.PlanDigest = plan.PlanID, plan.Digest
	return preview, nil
}

func (s *TrashFinalDeleteService) Apply(planID, confirmedEntryID string) (repositories.TrashFinalDeleteResult, error) {
	if s == nil || s.trash == nil || s.plans == nil || s.ops == nil || s.backups == nil {
		return repositories.TrashFinalDeleteResult{}, ErrTrashFinalDeleteAssociation
	}
	plan, err := s.plans.Load(planID)
	if err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	if plan.OperationType != "trash_final_delete" || plan.PayloadSchemaID != "trash_final_delete.v1" || !plan.ReadyToApply || plan.BackupRequirement != "none" || len(plan.Actions) != 1 || plan.Actions[0].ID != "entry" || plan.Actions[0].Action != "trash_final_delete" || !plan.Actions[0].Selectable || !plan.Actions[0].Destructive || len(plan.Actions[0].BackupSources) != 0 {
		return repositories.TrashFinalDeleteResult{}, ErrMaintenancePlanInvalid
	}
	var payload TrashFinalDeletePlanPayload
	decoder := json.NewDecoder(bytes.NewReader(plan.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return repositories.TrashFinalDeleteResult{}, errors.Join(ErrMaintenancePlanInvalid, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return repositories.TrashFinalDeleteResult{}, ErrMaintenancePlanInvalid
	}
	entry := payload.Snapshot.Entry
	if confirmedEntryID == "" || confirmedEntryID != entry.ID {
		return repositories.TrashFinalDeleteResult{}, ErrTrashFinalDeleteConfirmation
	}
	current, err := s.trash.Load(entry.ID)
	if err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	if current.Status != "sealed" && current.Status != "final_deleting" && current.Status != "final_deleted" {
		return repositories.TrashFinalDeleteResult{}, repositories.ErrTrashFinalDeleteState
	}
	original, err := s.verifyOriginalAssociation(current)
	if err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	if original.ApplyBackupID != payload.ApplyBackupID || original.RehearsalBackupID != payload.RehearsalBackupID {
		return repositories.TrashFinalDeleteResult{}, ErrTrashFinalDeleteAssociation
	}
	op, err := s.findOrCreateOperation(plan, entry.ID)
	if err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	if err := s.ops.HasExclusivePlanClaim(planID, op.ID); err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	if current.Status == "final_deleted" {
		if current.FinalDeletePlanID != planID || current.FinalDeleteOperationID != op.ID {
			return repositories.TrashFinalDeleteResult{}, repositories.ErrTrashFinalDeleteState
		}
		result := repositories.TrashFinalDeleteResult{EntryID: entry.ID, PlanID: planID, OperationID: op.ID, DeletedFiles: current.FinalDeleteFilesDeleted, TotalFiles: current.FinalDeleteFilesTotal, Status: current.Status}
		if op.Status != "verified" {
			if err := s.finishOperation(&op, result, nil); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	if op.Status == "verified" {
		return repositories.TrashFinalDeleteResult{}, repositories.ErrTrashFinalDeleteState
	}
	if err := s.setApplying(&op); err != nil {
		return repositories.TrashFinalDeleteResult{}, err
	}
	result, deleteErr := s.trash.DeleteFinal(payload.Snapshot, planID, op.ID, func(deleted int64) error {
		state, _ := json.Marshal(struct {
			DeletedFiles int64 `json:"deleted_files"`
		}{deleted})
		op.Actions[0].AfterState = state
		op.Revision++
		return s.ops.Update(op)
	})
	finishErr := s.finishOperation(&op, result, deleteErr)
	return result, errors.Join(deleteErr, finishErr)
}

func (s *TrashFinalDeleteService) verifyOriginalAssociation(entry models.TrashEntry) (models.MaintenanceOperation, error) {
	op, err := s.ops.Load(entry.OperationID)
	if err != nil {
		return op, errors.Join(ErrTrashFinalDeleteAssociation, err)
	}
	if op.Status != "verified" || op.Type != entry.OperationType || op.ID != entry.OperationID {
		return op, ErrTrashFinalDeleteAssociation
	}
	if op.ApplyBackupID == "" && op.RehearsalBackupID == "" {
		if entry.OperationType == "delete" && op.PlanID == "" && op.PlanDigest == "" && (op.Target == entry.ID || op.Target == entry.OriginalRelativePath) {
			return op, nil
		}
		return op, ErrTrashFinalDeleteAssociation
	}
	if op.ApplyBackupID == "" || op.PlanID == "" || op.PlanDigest == "" {
		return op, ErrTrashFinalDeleteAssociation
	}
	for _, link := range []struct{ id, role, state string }{{op.ApplyBackupID, "apply", "point_in_time_verified"}, {op.RehearsalBackupID, "rehearsal", "rehearsal_verified"}} {
		if link.id == "" {
			continue
		}
		backup, err := s.backups.Load(link.id)
		if err != nil {
			return op, errors.Join(ErrTrashFinalDeleteAssociation, err)
		}
		if backup.Registry.Role != link.role || backup.Registry.State != link.state || backup.Bundle.PlanID != op.PlanID || !backupCoversTrashSource(backup.Bundle.Sources, entry.OriginalRelativePath) {
			return op, ErrTrashFinalDeleteAssociation
		}
	}
	return op, nil
}

func backupCoversTrashSource(sources []string, relative string) bool {
	for _, source := range sources {
		if source == relative || strings.HasPrefix(relative, source+"/") {
			return true
		}
	}
	return false
}

func (s *TrashFinalDeleteService) findOrCreateOperation(plan models.MaintenancePlanEnvelope, entryID string) (models.MaintenanceOperation, error) {
	records, err := s.ops.List()
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	var existing *models.MaintenanceOperation
	for i := range records {
		if records[i].PlanID == plan.PlanID {
			if existing != nil {
				return models.MaintenanceOperation{}, repositories.ErrMaintenancePlanClaimed
			}
			existing = &records[i]
		}
	}
	if existing != nil {
		if existing.PlanDigest != plan.Digest || existing.Target != entryID || existing.Type != "trash_final_delete" || len(existing.Actions) != 1 {
			return models.MaintenanceOperation{}, repositories.ErrMaintenancePlanClaimed
		}
		return *existing, nil
	}
	id, err := s.newID()
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	beforeTrash, err := s.inspector.observe(s.inspector.TrashRoot, entryID+"/payload")
	if err != nil || !beforeTrash.Exists {
		return models.MaintenanceOperation{}, errors.Join(repositories.ErrTrashChanged, err)
	}
	intendedTrash := models.MaintenanceSnapshotEntry{Path: beforeTrash.Path}
	beforeState, _ := json.Marshal(models.MaintenanceSnapshot{Trash: &beforeTrash})
	intendedState, _ := json.Marshal(models.MaintenanceSnapshot{Trash: &intendedTrash})
	op := models.MaintenanceOperation{Version: 1, ID: id, Revision: 1, StartedAt: s.now().UTC(), Target: entryID, PlanID: plan.PlanID, PlanDigest: plan.Digest, Type: "trash_final_delete", Status: "prepared", Actions: []models.MaintenanceOperationAction{{ID: "entry", Action: "trash_final_delete", Status: "pending", BeforeState: beforeState, IntendedState: intendedState}}}
	if err := s.ops.Create(op); err != nil {
		return op, err
	}
	return op, nil
}

func (s *TrashFinalDeleteService) setApplying(op *models.MaintenanceOperation) error {
	op.Status = "applying"
	op.Actions[0].Status = "applying"
	op.Error, op.Result = "", ""
	op.CompletedAt = nil
	op.Revision++
	return s.ops.Update(*op)
}

func (s *TrashFinalDeleteService) finishOperation(op *models.MaintenanceOperation, result repositories.TrashFinalDeleteResult, cause error) error {
	if cause == nil {
		op.Status, op.Result, op.Actions[0].Status = "verified", "verified", "verified"
		op.Error, op.Actions[0].Error = "", ""
	} else {
		op.Status, op.Result, op.Actions[0].Status = "partial_failure", "partial_failure", "failed"
		op.Error, op.Actions[0].Error = cause.Error(), cause.Error()
	}
	state, _ := json.Marshal(result)
	op.Actions[0].AfterState = state
	completed := s.now().UTC()
	op.CompletedAt = &completed
	op.Revision++
	return s.ops.Update(*op)
}

func newTrashFinalDeleteOperationID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("final-delete operation ID: %w", err)
	}
	return "final_" + hex.EncodeToString(random[:]), nil
}
