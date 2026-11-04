package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/models"
	"backend/repositories"
)

type LegacyIRCutoverApplyService struct {
	plans      *LegacyIRCutoverPlanService
	operations *repositories.MaintenanceOperationRepository
	backup     *BackupService
	trash      *repositories.TrashRepository
	inspector  MaintenanceFilesystemInspector
	newID      func(string) (string, error)
}

func NewLegacyIRCutoverApplyService(plans *LegacyIRCutoverPlanService, operations *repositories.MaintenanceOperationRepository, backup *BackupService, trash *repositories.TrashRepository, runtimeRoot, trashRoot string) *LegacyIRCutoverApplyService {
	return &LegacyIRCutoverApplyService{plans: plans, operations: operations, backup: backup, trash: trash, inspector: MaintenanceFilesystemInspector{RuntimeRoot: runtimeRoot, TrashRoot: trashRoot}, newID: newCleanupID}
}

// Apply never resumes a partial move. The coordinator writes intent first and
// the backup service seals a separate point-in-time snapshot before Trash.Move.
func (s *LegacyIRCutoverApplyService) Apply(request models.LegacyIRCutoverApplyRequest) (models.MaintenanceOperation, error) {
	if s == nil || s.plans == nil || s.plans.plans == nil || s.operations == nil || s.backup == nil || s.trash == nil || s.newID == nil || request.Validate() != nil {
		return models.MaintenanceOperation{}, ErrLegacyIRCutoverInvalid
	}
	plan, err := s.plans.Load(request.PlanID)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if plan.Digest != request.PlanDigest {
		return models.MaintenanceOperation{}, ErrMaintenancePlanStale
	}
	if s.plans.root != s.inspector.RuntimeRoot {
		return models.MaintenanceOperation{}, ErrLegacyIRCutoverInvalid
	}
	payload, err := decodeLegacyIRCutoverPayload(plan.Payload)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if err := s.plans.verifyMigration(payload.MigrationOperationID); err != nil {
		return models.MaintenanceOperation{}, err
	}
	actionIDs := []string{legacyIRCutoverActionID}
	if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, actionIDs); err != nil {
		return models.MaintenanceOperation{}, err
	}
	rehearsal, err := s.backup.Detail(request.RehearsalBackupID)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if err := s.plans.plans.VerifyRehearsalBackup(plan.PlanID, plan.Digest, actionIDs, rehearsal, true); err != nil {
		return models.MaintenanceOperation{}, err
	}
	opID, err := s.newID("legacy_cutover_")
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	entryID, err := s.newID("trash_")
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	beforeRuntime, err := s.inspector.observe(s.inspector.RuntimeRoot, "ir-signals")
	if err != nil || !beforeRuntime.Exists || beforeRuntime.Kind != "directory" {
		return models.MaintenanceOperation{}, errors.Join(ErrLegacyIRCutoverStale, err)
	}
	beforeTrash, err := s.inspector.observe(s.inspector.TrashRoot, entryID+"/payload")
	if err != nil || beforeTrash.Exists {
		return models.MaintenanceOperation{}, errors.Join(ErrLegacyIRCutoverStale, err)
	}
	afterRuntime := beforeRuntime
	afterRuntime.Exists, afterRuntime.Kind, afterRuntime.SHA256 = false, "", ""
	afterTrash := beforeRuntime
	afterTrash.Path = entryID + "/payload"
	before := models.MaintenanceSnapshot{Runtime: &beforeRuntime, Trash: &beforeTrash}
	intended := models.MaintenanceSnapshot{Runtime: &afterRuntime, Trash: &afterTrash}
	beforeJSON, _ := json.Marshal(before)
	intendedJSON, _ := json.Marshal(intended)
	work := []MaintenanceActionWork{{ID: legacyIRCutoverActionID, Action: "trash_directory", BeforeState: beforeJSON, IntendedState: intendedJSON,
		Mutate: func() error {
			current, err := s.inspector.observe(s.inspector.RuntimeRoot, "ir-signals")
			if err != nil || current != beforeRuntime {
				return errors.Join(ErrLegacyIRCutoverStale, err)
			}
			if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, actionIDs); err != nil {
				return err
			}
			_, err = s.trash.Move(repositories.TrashMoveRequest{EntryID: entryID, SourceRelativePath: "ir-signals", OriginalID: "ir-signals", TargetType: "legacy_ir_directory", Reason: "v0.11.0_step17", OperationType: "legacy_ir_cutover", OperationID: opID})
			return err
		},
		Readback: func() (json.RawMessage, error) {
			entry, err := s.trash.Load(entryID)
			if err != nil {
				return nil, err
			}
			if entry.Status != "sealed" || !entry.RestoreAvailable || entry.OperationID != opID || entry.OriginalRelativePath != "ir-signals" || entry.BeforeInventory.Kind != "directory" || entry.BeforeInventory.FileCount != plan.Actions[0].BackupFileCount || entry.BeforeInventory.TotalSize != plan.Actions[0].BackupTotalSize {
				return nil, fmt.Errorf("legacy cutover Trash entry readback mismatch")
			}
			if err := verifyLegacyIRTrashFiles(s.inspector.TrashRoot, entryID, plan.Actions[0].BackupFiles); err != nil {
				return nil, err
			}
			runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, "ir-signals")
			if err != nil {
				return nil, err
			}
			trash, err := s.inspector.observe(s.inspector.TrashRoot, entryID+"/payload")
			if err != nil {
				return nil, err
			}
			actual := models.MaintenanceSnapshot{Runtime: &runtime, Trash: &trash}
			if !sameSnapshot(actual, intended) {
				return nil, fmt.Errorf("legacy cutover filesystem readback mismatch")
			}
			return json.Marshal(actual)
		},
	}}
	draft := models.MaintenanceOperation{ID: opID, Target: "ir-signals", Type: "legacy_ir_cutover", PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: request.RehearsalBackupID}
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
		if _, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, actionIDs); err != nil {
			return updated, err
		}
		if err := s.plans.verifyMigration(payload.MigrationOperationID); err != nil {
			return updated, err
		}
		return updated, nil
	})
}

func verifyLegacyIRTrashFiles(trashRoot, entryID string, files []models.BackupFile) error {
	for _, file := range files {
		if !strings.HasPrefix(file.Path, "ir-signals/") {
			return ErrLegacyIRCutoverInvalid
		}
		p := filepath.Join(trashRoot, entryID, "payload", strings.TrimPrefix(file.Path, "ir-signals/"))
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
			return errors.Join(ErrLegacyIRCutoverStale, err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			return ErrLegacyIRCutoverStale
		}
	}
	return nil
}
