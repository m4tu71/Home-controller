package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"backend/models"
	"backend/repositories"
)

const legacyIRCutoverActionID = "archive_legacy_ir_signals"

var ErrLegacyIRCutoverInvalid = errors.New("invalid legacy IR cutover request or plan")
var ErrLegacyIRCutoverStale = errors.New("legacy IR cutover source or migration changed")

type legacyIRCutoverOperationReader interface {
	Load(string) (models.MaintenanceOperation, error)
}

type legacyIRCutoverMigrationVerifier interface {
	Verify(models.MigrationVerifyRequest) (models.MigrationVerifyResult, error)
}

type LegacyIRCutoverPlanService struct {
	root       string
	plans      *MaintenancePlanService
	operations legacyIRCutoverOperationReader
	verify     legacyIRCutoverMigrationVerifier
}

func NewLegacyIRCutoverPlanService(root string, plans *MaintenancePlanService, operations legacyIRCutoverOperationReader, verify legacyIRCutoverMigrationVerifier) *LegacyIRCutoverPlanService {
	return &LegacyIRCutoverPlanService{root: root, plans: plans, operations: operations, verify: verify}
}

func (s *LegacyIRCutoverPlanService) Create(request models.LegacyIRCutoverDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil || s.plans.backups == nil || !filepath.IsAbs(s.root) || request.Validate() != nil {
		return models.MaintenancePlanEnvelope{}, ErrLegacyIRCutoverInvalid
	}
	if err := s.verifyMigration(request.MigrationOperationID); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	inv, err := s.plans.backups.InspectSources([]string{"ir-signals"})
	if err != nil {
		return models.MaintenancePlanEnvelope{}, errors.Join(ErrLegacyIRCutoverStale, err)
	}
	if err := validateLegacyIRSourceClasses(request.Sources, inv); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	sources := append([]models.LegacyIRSourceClassification(nil), request.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	payload, err := json.Marshal(models.LegacyIRCutoverPlanPayload{MigrationOperationID: request.MigrationOperationID, SourcePath: "ir-signals", Sources: sources})
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	draft := models.MaintenancePlanEnvelope{OperationType: "legacy_ir_cutover", PayloadSchemaID: "legacy_ir_cutover.v1", Payload: payload, ReadyToApply: true, BackupRequirement: "rehearsal_and_apply", Actions: []models.MaintenancePlanAction{{ID: legacyIRCutoverActionID, Action: "trash_directory", Selectable: true, Destructive: true, BackupSources: []string{"ir-signals"}}}}
	if err := ValidateLegacyIRCutoverPlan(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return s.plans.Create(draft)
}

func (s *LegacyIRCutoverPlanService) Load(id string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrLegacyIRCutoverInvalid
	}
	plan, err := s.plans.Load(id)
	if err != nil {
		return plan, err
	}
	if err := ValidateLegacyIRCutoverPlan(plan); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return plan, nil
}

func ValidateLegacyIRCutoverPlan(plan models.MaintenancePlanEnvelope) error {
	if plan.OperationType != "legacy_ir_cutover" || plan.PayloadSchemaID != "legacy_ir_cutover.v1" || !plan.ReadyToApply || plan.BackupRequirement != "rehearsal_and_apply" || len(plan.Actions) != 1 {
		return ErrLegacyIRCutoverInvalid
	}
	a := plan.Actions[0]
	if a.ID != legacyIRCutoverActionID || a.Action != "trash_directory" || !a.Selectable || !a.Destructive || len(a.BackupSources) != 1 || a.BackupSources[0] != "ir-signals" {
		return ErrLegacyIRCutoverInvalid
	}
	payload, err := decodeLegacyIRCutoverPayload(plan.Payload)
	if err != nil || payload.SourcePath != "ir-signals" || payload.MigrationOperationID == "" || len(payload.Sources) == 0 {
		return ErrLegacyIRCutoverInvalid
	}
	previous := ""
	for _, source := range payload.Sources {
		if source.Path <= previous || path.Dir(source.Path) != "ir-signals" || !strings.HasSuffix(source.Path, ".json") || !validSourcePath(source.Path) || (source.Classification != "intentionally_skipped" && source.Classification != "invalid") {
			return ErrLegacyIRCutoverInvalid
		}
		previous = source.Path
	}
	if plan.PlanID != "" {
		inv := repositories.BackupSourceInventory{Files: a.BackupFiles, Directories: a.BackupDirectories, FileCount: a.BackupFileCount, TotalSize: a.BackupTotalSize, InventoryHash: a.BackupInventoryHash}
		return validateLegacyIRSourceClasses(payload.Sources, inv)
	}
	return nil
}

func decodeLegacyIRCutoverPayload(raw json.RawMessage) (models.LegacyIRCutoverPlanPayload, error) {
	var payload models.LegacyIRCutoverPlanPayload
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&payload); err != nil {
		return payload, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return payload, ErrLegacyIRCutoverInvalid
	}
	return payload, nil
}

func validateLegacyIRSourceClasses(sources []models.LegacyIRSourceClassification, inv repositories.BackupSourceInventory) error {
	if len(inv.Directories) != 1 || inv.Directories[0] != "ir-signals" || len(inv.Files) == 0 || len(sources) != len(inv.Files) {
		return ErrLegacyIRCutoverInvalid
	}
	byPath := make(map[string]models.BackupFile, len(inv.Files))
	for _, file := range inv.Files {
		if path.Dir(file.Path) != "ir-signals" || !strings.HasSuffix(file.Path, ".json") || !validSourcePath(file.Path) {
			return ErrLegacyIRCutoverInvalid
		}
		byPath[file.Path] = file
	}
	seen := map[string]bool{}
	for _, source := range sources {
		file, ok := byPath[source.Path]
		if !ok || seen[source.Path] {
			return ErrLegacyIRCutoverInvalid
		}
		seen[source.Path] = true
		switch source.Classification {
		case "invalid":
			if file.JSONStatus != "empty" && file.JSONStatus != "invalid" {
				return ErrLegacyIRCutoverInvalid
			}
		case "intentionally_skipped":
			if file.JSONStatus != "valid" {
				return ErrLegacyIRCutoverInvalid
			}
		default:
			return ErrLegacyIRCutoverInvalid
		}
	}
	return nil
}

func (s *LegacyIRCutoverPlanService) verifyMigration(id string) error {
	if s.operations == nil || s.verify == nil {
		return ErrLegacyIRCutoverInvalid
	}
	op, err := s.operations.Load(id)
	if err != nil || op.Type != "migration" || op.Status != "verified" || op.RehearsalBackupID == "" || op.ApplyBackupID == "" || len(op.Actions) == 0 {
		return errors.Join(ErrLegacyIRCutoverStale, err)
	}
	for _, action := range op.Actions {
		if action.Status != "verified" {
			return ErrLegacyIRCutoverStale
		}
	}
	result, err := s.verify.Verify(models.MigrationVerifyRequest{OperationID: id})
	if err != nil || result.Status != "verified" || len(result.Targets) != len(op.Actions) {
		return errors.Join(ErrLegacyIRCutoverStale, err)
	}
	return nil
}
