package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"sort"

	"backend/models"
	"backend/repositories"
)

var ErrMigrationRollbackInvalid = errors.New("invalid migration rollback source or plan")

type migrationRollbackBackupReader interface {
	Detail(string) (models.BackupDetail, error)
}

type MigrationRollbackPlanService struct {
	plans      *MaintenancePlanService
	migration  *MigrationPlanService
	operations migrationVerifyOperationStore
	backups    migrationRollbackBackupReader
	inspector  MaintenanceFilesystemInspector
}

func NewMigrationRollbackPlanService(plans *MaintenancePlanService, migration *MigrationPlanService, operations *repositories.MaintenanceOperationRepository, backups *BackupService, root string) *MigrationRollbackPlanService {
	return &MigrationRollbackPlanService{plans: plans, migration: migration, operations: operations, backups: backups, inspector: MaintenanceFilesystemInspector{RuntimeRoot: root}}
}

func (s *MigrationRollbackPlanService) Load(id string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
	}
	plan, err := s.plans.Load(id)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if err := ValidateMigrationRollbackPlan(plan); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return plan, nil
}

func (s *MigrationRollbackPlanService) Create(request models.MigrationRollbackDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil || s.migration == nil || s.operations == nil || s.backups == nil || !filepath.IsAbs(s.inspector.RuntimeRoot) || request.Validate() != nil {
		return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
	}
	op, err := s.operations.Load(request.OperationID)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if op.Type != "migration" || op.PlanID == "" || op.PlanDigest == "" || len(op.Actions) == 0 {
		return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
	}
	migrationPlan, err := s.migration.Load(op.PlanID)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if migrationPlan.Digest != op.PlanDigest {
		return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
	}
	var source models.MigrationPlanPayload
	if err := json.Unmarshal(migrationPlan.Payload, &source); err != nil {
		return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationRollbackInvalid, err)
	}
	byID := map[string]models.MigrationPlanAction{}
	shared := map[string][]string{}
	for _, item := range source.Actions {
		byID[item.ActionID] = item
		seenShared := map[string]bool{}
		for _, w := range item.PlannedWrites {
			if w.Role != "operation" {
				shared[w.Path] = append(shared[w.Path], item.ActionID)
				seenShared[w.Path] = true
			}
		}
		for _, dep := range migrationIdentityDependencies(item) {
			if !seenShared[dep.Path] {
				shared[dep.Path] = append(shared[dep.Path], item.ActionID)
			}
		}
	}
	ids := make([]string, 0, len(op.Actions))
	for _, a := range op.Actions {
		ids = append(ids, a.ID)
	}
	if op.ApplyBackupID != "" {
		backup, err := s.backups.Detail(op.ApplyBackupID)
		if err != nil {
			return models.MaintenancePlanEnvelope{}, err
		}
		if backup.Registry.Role != "apply" || backup.Registry.State != "point_in_time_verified" || backup.Bundle.PlanID != op.PlanID || !sameStringSet(backup.Bundle.ActionIDs, ids) {
			return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
		}
	}
	payload := models.MigrationRollbackPlanPayload{MigrationOperationID: op.ID, MigrationPlanID: op.PlanID, MigrationPlanDigest: op.PlanDigest, ApplyBackupID: op.ApplyBackupID, Targets: make([]models.MigrationRollbackTarget, 0, len(op.Actions))}
	planActions := make([]models.MaintenancePlanAction, 0, len(op.Actions))
	ready := false
	for _, action := range op.Actions {
		item, ok := byID[action.ID]
		if !ok || item.Action != action.Action {
			return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
		}
		target := s.candidate(op, item, action, shared)
		payload.Targets = append(payload.Targets, target)
		planAction := models.MaintenancePlanAction{ID: action.ID, Action: "rollback_target", Selectable: target.Eligible, Destructive: target.Eligible}
		if target.Eligible {
			planAction.BackupSources = []string{target.TargetPath}
			ready = true
		}
		planActions = append(planActions, planAction)
	}
	for _, target := range payload.Targets {
		if !target.Eligible {
			continue
		}
		if s.plans.backups == nil {
			return models.MaintenancePlanEnvelope{}, ErrMigrationRollbackInvalid
		}
		inventory, err := s.plans.backups.InspectSources([]string{target.TargetPath})
		if err != nil || len(inventory.Files) != 1 || inventory.Files[0].Path != target.TargetPath || inventory.Files[0].SHA256 != target.CurrentSHA256 {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMaintenancePlanStale, err)
		}
	}
	sort.Slice(payload.Targets, func(i, j int) bool { return payload.Targets[i].ActionID < payload.Targets[j].ActionID })
	sort.Slice(planActions, func(i, j int) bool { return planActions[i].ID < planActions[j].ID })
	encoded, err := json.Marshal(payload)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	draft := models.MaintenancePlanEnvelope{OperationType: "migration_rollback", PayloadSchemaID: "migration_rollback.v1", Payload: encoded, ReadyToApply: ready, BackupRequirement: "apply", Actions: planActions}
	if err := ValidateMigrationRollbackPlan(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	issued, err := s.plans.Create(draft)
	if err != nil {
		return issued, err
	}
	for _, action := range issued.Actions {
		for _, target := range payload.Targets {
			if target.ActionID == action.ID && target.Eligible && (len(action.BackupFiles) != 1 || action.BackupFiles[0].Path != target.TargetPath || action.BackupFiles[0].SHA256 != target.CurrentSHA256) {
				return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanStale
			}
		}
	}
	return issued, nil
}

func (s *MigrationRollbackPlanService) candidate(op models.MaintenanceOperation, item models.MigrationPlanAction, action models.MaintenanceOperationAction, shared map[string][]string) models.MigrationRollbackTarget {
	v := models.MigrationRollbackTarget{ActionID: item.ActionID, TargetPath: item.TargetBefore.Path, Reason: "provenance_unconfirmed", ExpectedAfterState: "target_unchanged", Dependencies: []models.MigrationRollbackDependency{}}
	deps := map[string]models.MigrationRollbackDependency{}
	for _, w := range item.PlannedWrites {
		if w.Role == "operation" {
			continue
		}
		ids := append([]string(nil), shared[w.Path]...)
		sort.Strings(ids)
		deps[w.Path] = models.MigrationRollbackDependency{Path: w.Path, Kind: w.Kind, Role: w.Role, CreatedByApply: true, SharedByActionIDs: ids}
	}
	for _, dep := range migrationIdentityDependencies(item) {
		if _, exists := deps[dep.Path]; exists {
			continue
		}
		ids := append([]string(nil), shared[dep.Path]...)
		sort.Strings(ids)
		dep.SharedByActionIDs = ids
		deps[dep.Path] = dep
	}
	for _, dep := range deps {
		v.Dependencies = append(v.Dependencies, dep)
	}
	sort.Slice(v.Dependencies, func(i, j int) bool { return v.Dependencies[i].Path < v.Dependencies[j].Path })
	var before, intended, after models.MaintenanceSnapshot
	if json.Unmarshal(action.BeforeState, &before) != nil || json.Unmarshal(action.IntendedState, &intended) != nil || before.Runtime == nil || intended.Runtime == nil || before.Runtime.Path != v.TargetPath || intended.Runtime.Path != v.TargetPath || before.Runtime.Exists || !intended.Runtime.Exists || intended.Runtime.Kind != "file" || !validMigrationHash(intended.Runtime.SHA256) {
		return v
	}
	v.ApplySHA256 = intended.Runtime.SHA256
	e, err := s.inspector.observe(s.inspector.RuntimeRoot, v.TargetPath)
	if err != nil {
		v.Reason = "unsafe_or_unreadable_target"
		return v
	}
	v.CurrentExists, v.CurrentKind, v.CurrentSHA256 = e.Exists, e.Kind, e.SHA256
	if !e.Exists {
		v.Reason = "missing_target"
		return v
	}
	if e.Kind != "file" {
		v.Reason = "invalid_target_type"
		return v
	}
	if e.SHA256 != v.ApplySHA256 {
		v.Reason = "modified_target"
		return v
	}
	if op.ApplyBackupID == "" {
		v.Reason = "apply_backup_missing"
		return v
	}
	if action.Status != "verified" && action.Status != "applied" {
		return v
	}
	if !migrationMutationReturned(op, action.ID) {
		return v
	}
	if action.Status == "verified" {
		if json.Unmarshal(action.AfterState, &after) != nil || after.Runtime == nil || *after.Runtime != *intended.Runtime {
			return v
		}
	}
	v.Eligible = true
	v.Reason = "eligible"
	v.ExpectedAfterState = "target_moved_to_trash"
	return v
}

func migrationIdentityDependencies(item models.MigrationPlanAction) []models.MigrationRollbackDependency {
	kind, roomID, deviceID, _, ok := migrationTarget(item.TargetBefore.Path)
	if !ok {
		return nil
	}
	base := path.Join("rooms", roomID)
	irBase := path.Join(base, "devices", deviceID, "adapters", "ir")
	deps := []models.MigrationRollbackDependency{
		{Path: path.Join(base, "room.json"), Kind: "json", Role: "room"},
		{Path: path.Join(base, "devices", deviceID, "device.json"), Kind: "json", Role: "device"},
		{Path: path.Join(irBase, "ir_config.json"), Kind: "json", Role: "ir_config"},
	}
	if kind == "state" {
		deps = append(deps, models.MigrationRollbackDependency{Path: path.Join(irBase, "state_schema.json"), Kind: "json", Role: "state_schema"})
	}
	return deps
}

func migrationMutationReturned(op models.MaintenanceOperation, actionID string) bool {
	for _, e := range op.Events {
		if e.ActionID == actionID && e.Kind == "mutation_returned" {
			return true
		}
	}
	return false
}

func ValidateMigrationRollbackPlan(plan models.MaintenancePlanEnvelope) error {
	if plan.OperationType != "migration_rollback" || plan.PayloadSchemaID != "migration_rollback.v1" || plan.BackupRequirement != "apply" || len(plan.Actions) == 0 {
		return ErrMigrationRollbackInvalid
	}
	d := json.NewDecoder(bytes.NewReader(plan.Payload))
	d.DisallowUnknownFields()
	var payload models.MigrationRollbackPlanPayload
	if d.Decode(&payload) != nil || d.Decode(new(any)) != io.EOF || !planActionIDPattern.MatchString(payload.MigrationOperationID) || !planIDPattern.MatchString(payload.MigrationPlanID) || !validPlanDigest(payload.MigrationPlanDigest) || (payload.ApplyBackupID != "" && !planActionIDPattern.MatchString(payload.ApplyBackupID)) || len(payload.Targets) != len(plan.Actions) {
		return ErrMigrationRollbackInvalid
	}
	byID := map[string]models.MaintenancePlanAction{}
	for _, a := range plan.Actions {
		if byID[a.ID].ID != "" {
			return ErrMigrationRollbackInvalid
		}
		byID[a.ID] = a
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	ready := false
	for _, target := range payload.Targets {
		a, ok := byID[target.ActionID]
		_, _, _, _, validTarget := migrationTarget(target.TargetPath)
		if !ok || seen[target.ActionID] || paths[target.TargetPath] || !validTarget || !validSourcePath(target.TargetPath) || a.Action != "rollback_target" || a.Selectable != target.Eligible || a.Destructive != target.Eligible || !validRollbackReason(target.Reason) || !validRollbackHashOrEmpty(target.ApplySHA256) || !validRollbackHashOrEmpty(target.CurrentSHA256) {
			return ErrMigrationRollbackInvalid
		}
		if target.CurrentExists && (target.CurrentKind != "file" && target.CurrentKind != "directory" || target.CurrentSHA256 == "") {
			return ErrMigrationRollbackInvalid
		}
		if !target.CurrentExists && (target.CurrentKind != "" || target.CurrentSHA256 != "") {
			return ErrMigrationRollbackInvalid
		}
		if target.Eligible {
			if target.Reason != "eligible" || target.ExpectedAfterState != "target_moved_to_trash" || target.ApplySHA256 == "" || target.CurrentSHA256 != target.ApplySHA256 || !target.CurrentExists || target.CurrentKind != "file" || len(a.BackupSources) != 1 || a.BackupSources[0] != target.TargetPath {
				return ErrMigrationRollbackInvalid
			}
			ready = true
		} else if target.Reason == "eligible" || target.ExpectedAfterState != "target_unchanged" || len(a.BackupSources) != 0 {
			return ErrMigrationRollbackInvalid
		}
		depPaths := map[string]bool{}
		for _, dep := range target.Dependencies {
			if !validSourcePath(dep.Path) || depPaths[dep.Path] || dep.Path == target.TargetPath || !pathIsAncestorOrSibling(dep.Path, target.TargetPath) || len(dep.SharedByActionIDs) == 0 || (dep.Kind != "json" && dep.Kind != "directory") || (dep.Kind == "directory" && dep.Role != "directory") {
				return ErrMigrationRollbackInvalid
			}
			depPaths[dep.Path] = true
			for _, id := range dep.SharedByActionIDs {
				if !planActionIDPattern.MatchString(id) {
					return ErrMigrationRollbackInvalid
				}
			}
		}
		seen[target.ActionID] = true
		paths[target.TargetPath] = true
	}
	if plan.ReadyToApply != ready {
		return ErrMigrationRollbackInvalid
	}
	return nil
}

func validRollbackHashOrEmpty(hash string) bool { return hash == "" || validMigrationHash(hash) }
func validRollbackReason(reason string) bool {
	switch reason {
	case "eligible", "provenance_unconfirmed", "unsafe_or_unreadable_target", "missing_target", "invalid_target_type", "modified_target", "apply_backup_missing":
		return true
	default:
		return false
	}
}
func pathIsAncestorOrSibling(dependency, target string) bool {
	return path.Dir(dependency) == path.Dir(target) || backupPathContains(dependency, path.Dir(target)) || backupPathContains(path.Dir(dependency), target)
}
