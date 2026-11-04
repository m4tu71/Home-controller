package services

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"backend/models"
	"backend/repositories"
)

var (
	ErrMaintenancePlanInvalid       = errors.New("invalid maintenance plan")
	ErrMaintenancePlanStale         = errors.New("maintenance plan sources changed")
	ErrMaintenancePlanActionMissing = errors.New("maintenance plan action not found")
	ErrMaintenancePlanNotReady      = errors.New("maintenance plan is not ready")
	ErrMaintenancePlanUnsupported   = errors.New("unsupported maintenance plan operation or schema")
	planIDPattern                   = regexp.MustCompile(`^plan_[a-f0-9]{32}$`)
	planActionIDPattern             = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
)

var supportedPlanSchemas = map[string]string{
	"migration.v1":          "migration",
	"migration.v2":          "migration",
	"migration_rollback.v1": "migration_rollback",
	"cleanup.v1":            "cleanup",
	"trash_final_delete.v1": "trash_final_delete",
	"legacy_ir_cutover.v1":  "legacy_ir_cutover",
}

type MaintenancePlanStore interface {
	Create(string, json.RawMessage) error
	Load(string) (json.RawMessage, error)
}

type BackupSourceInspector interface {
	InspectSources([]string) (repositories.BackupSourceInventory, error)
}

type MaintenanceBackupSelection struct {
	Plan          models.MaintenancePlanEnvelope
	ActionIDs     []string
	Sources       []string
	Files         []models.BackupFile
	Directories   []string
	FileCount     int64
	TotalSize     int64
	InventoryHash string
}

type MaintenancePlanService struct {
	plans   MaintenancePlanStore
	backups BackupSourceInspector
	now     func() time.Time
	newID   func() (string, error)
}

func NewMaintenancePlanService(plans MaintenancePlanStore, backups BackupSourceInspector) *MaintenancePlanService {
	return &MaintenancePlanService{plans: plans, backups: backups, now: func() time.Time { return time.Now().UTC() }, newID: newMaintenancePlanID}
}

// Create issues the ID and timestamp, captures each action's backup inventory,
// canonicalizes its payload, computes its digest, then persists one immutable plan.
func (s *MaintenancePlanService) Create(draft models.MaintenancePlanEnvelope) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	if draft.PlanID != "" || draft.Digest != "" || !draft.CreatedAt.IsZero() {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	draft.Actions = append([]models.MaintenancePlanAction(nil), draft.Actions...)
	for i := range draft.Actions {
		draft.Actions[i].BackupSources = append([]string(nil), draft.Actions[i].BackupSources...)
	}
	draft.Version = models.MaintenancePlanVersion
	if err := validatePlanDraft(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	canonicalPayload, err := canonicalJSON(draft.Payload)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	draft.Payload = canonicalPayload
	for i := range draft.Actions {
		action := &draft.Actions[i]
		if len(action.BackupSources) == 0 {
			clearPlanActionInventory(action)
			continue
		}
		if s.backups == nil {
			return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
		}
		sort.Strings(action.BackupSources)
		inv, err := s.backups.InspectSources(action.BackupSources)
		if err != nil {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMaintenancePlanStale, err)
		}
		action.BackupFiles = cloneBackupFiles(inv.Files)
		action.BackupDirectories = append([]string{}, inv.Directories...)
		action.BackupInventoryHash = inv.InventoryHash
		action.BackupFileCount = inv.FileCount
		action.BackupTotalSize = inv.TotalSize
	}
	id, err := s.newID()
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	draft.PlanID = id
	draft.CreatedAt = s.now().UTC()
	draft.Digest, err = maintenancePlanDigest(draft)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if err := validatePlanEnvelope(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if err := s.plans.Create(draft.PlanID, encoded); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return draft, nil
}

func (s *MaintenancePlanService) Load(planID string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil || !planIDPattern.MatchString(planID) {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	data, err := s.plans.Load(planID)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	var plan models.MaintenancePlanEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return models.MaintenancePlanEnvelope{}, errors.Join(repositories.ErrMaintenanceCorrupt, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return models.MaintenancePlanEnvelope{}, repositories.ErrMaintenanceCorrupt
	}
	if err := validatePlanEnvelope(plan); err != nil || plan.PlanID != planID {
		return models.MaintenancePlanEnvelope{}, errors.Join(repositories.ErrMaintenanceCorrupt, err)
	}
	computed, err := maintenancePlanDigest(plan)
	if err != nil || computed != plan.Digest {
		return models.MaintenancePlanEnvelope{}, errors.Join(repositories.ErrMaintenanceCorrupt, err)
	}
	return plan, nil
}

// VerifyBackupSelection validates the saved plan, selected action set and the
// current source inventories before any rehearsal bundle is created.
func (s *MaintenancePlanService) VerifyBackupSelection(planID, digest string, actionIDs []string) (MaintenanceBackupSelection, error) {
	return s.verifySelection(planID, digest, actionIDs, "rehearsal")
}

func (s *MaintenancePlanService) VerifyApplySelection(planID, digest string, actionIDs []string) (MaintenanceBackupSelection, error) {
	return s.verifySelection(planID, digest, actionIDs, "apply")
}

func (s *MaintenancePlanService) verifySelection(planID, digest string, actionIDs []string, purpose string) (MaintenanceBackupSelection, error) {
	if s == nil || s.backups == nil {
		return MaintenanceBackupSelection{}, ErrMaintenancePlanInvalid
	}
	plan, err := s.Load(planID)
	if err != nil {
		return MaintenanceBackupSelection{}, err
	}
	if digest != plan.Digest {
		return MaintenanceBackupSelection{}, ErrMaintenancePlanStale
	}
	if !plan.ReadyToApply || !planRequiresBackup(plan, purpose) {
		return MaintenanceBackupSelection{}, ErrMaintenancePlanNotReady
	}
	if err := validateActionSelection(actionIDs); err != nil {
		return MaintenanceBackupSelection{}, err
	}
	byID := make(map[string]models.MaintenancePlanAction, len(plan.Actions))
	for _, action := range plan.Actions {
		byID[action.ID] = action
	}
	selected := append([]string(nil), actionIDs...)
	sort.Strings(selected)
	selection := MaintenanceBackupSelection{Plan: plan, ActionIDs: selected}
	pathSet, fileSet, dirSet := map[string]bool{}, map[string]models.BackupFile{}, map[string]bool{}
	for _, id := range selected {
		action, ok := byID[id]
		if !ok {
			return MaintenanceBackupSelection{}, ErrMaintenancePlanActionMissing
		}
		if !action.Selectable || len(action.BackupSources) == 0 {
			return MaintenanceBackupSelection{}, ErrMaintenancePlanNotReady
		}
		current, err := s.backups.InspectSources(action.BackupSources)
		if err != nil || !sameMaintenanceInventory(action, current) {
			return MaintenanceBackupSelection{}, errors.Join(ErrMaintenancePlanStale, err)
		}
		for _, source := range action.BackupSources {
			pathSet[source] = true
		}
		for _, file := range action.BackupFiles {
			if previous, exists := fileSet[file.Path]; exists && previous != file {
				return MaintenanceBackupSelection{}, ErrMaintenancePlanStale
			}
			fileSet[file.Path] = file
		}
		for _, directory := range action.BackupDirectories {
			dirSet[directory] = true
		}
	}
	for path := range pathSet {
		selection.Sources = append(selection.Sources, path)
	}
	sort.Strings(selection.Sources)
	for i, parent := range selection.Sources {
		for j, child := range selection.Sources {
			if i != j && backupPathContains(parent, child) {
				return MaintenanceBackupSelection{}, ErrMaintenancePlanNotReady
			}
		}
	}
	for _, file := range fileSet {
		selection.Files = append(selection.Files, file)
		selection.TotalSize += file.Size
	}
	for directory := range dirSet {
		selection.Directories = append(selection.Directories, directory)
	}
	sort.Slice(selection.Files, func(i, j int) bool { return selection.Files[i].Path < selection.Files[j].Path })
	sort.Strings(selection.Directories)
	selection.FileCount = int64(len(selection.Files))
	selection.InventoryHash, err = repositories.ComputeBackupInventoryHash(selection.Files, selection.Directories)
	if err != nil {
		return MaintenanceBackupSelection{}, err
	}
	return selection, nil
}

func (s *MaintenancePlanService) VerifyRehearsalBackup(planID, digest string, actionIDs []string, backup models.BackupDetail, requireRestore bool) error {
	selection, err := s.VerifyBackupSelection(planID, digest, actionIDs)
	if err != nil {
		return err
	}
	stateOK := backup.Registry.State == "snapshot_verified"
	if requireRestore {
		stateOK = backup.Registry.State == "rehearsal_verified"
	}
	if backup.Registry.Role != "rehearsal" || !stateOK || backup.Bundle.PlanID != planID || !sameStringSet(backup.Bundle.ActionIDs, selection.ActionIDs) || backup.Bundle.InventoryHash != selection.InventoryHash || backup.Bundle.FileCount != selection.FileCount || backup.Bundle.TotalSize != selection.TotalSize {
		return ErrMaintenancePlanStale
	}
	return nil
}

func (s *MaintenancePlanService) VerifyApplyBackup(planID, digest string, actionIDs []string, backup models.BackupDetail) error {
	selection, err := s.VerifyApplySelection(planID, digest, actionIDs)
	if err != nil {
		return err
	}
	if backup.Registry.Role != "apply" || backup.Registry.State != "point_in_time_verified" || backup.Bundle.PlanID != planID || !sameStringSet(backup.Bundle.ActionIDs, selection.ActionIDs) || backup.Bundle.InventoryHash != selection.InventoryHash || backup.Bundle.FileCount != selection.FileCount || backup.Bundle.TotalSize != selection.TotalSize {
		return ErrMaintenancePlanStale
	}
	return nil
}

func validatePlanDraft(plan models.MaintenancePlanEnvelope) error {
	if plan.Version != models.MaintenancePlanVersion || plan.PlanID != "" || plan.Digest != "" || !plan.CreatedAt.IsZero() {
		return fmt.Errorf("%w: caller-supplied plan identity or unsupported envelope version", ErrMaintenancePlanInvalid)
	}
	if !supportedPlan(plan.OperationType, plan.PayloadSchemaID) {
		return fmt.Errorf("%w: %w", ErrMaintenancePlanInvalid, ErrMaintenancePlanUnsupported)
	}
	if !validBackupRequirementForOperation(plan.OperationType, plan.BackupRequirement) || len(plan.Actions) == 0 || !validJSONObject(plan.Payload) {
		return fmt.Errorf("%w: invalid requirement, actions, or payload", ErrMaintenancePlanInvalid)
	}
	if plan.BackupRequirement != "none" {
		for _, action := range plan.Actions {
			if action.Selectable && action.Destructive && len(action.BackupSources) == 0 {
				return fmt.Errorf("%w: destructive action %s lacks backup sources", ErrMaintenancePlanInvalid, action.ID)
			}
		}
	}
	return validatePlanActions(plan.Actions, false)
}

func validatePlanEnvelope(plan models.MaintenancePlanEnvelope) error {
	if plan.Version != models.MaintenancePlanVersion || !planIDPattern.MatchString(plan.PlanID) || plan.CreatedAt.IsZero() || !validBackupRequirementForOperation(plan.OperationType, plan.BackupRequirement) || !validJSONObject(plan.Payload) || !validPlanDigest(plan.Digest) {
		return fmt.Errorf("%w: invalid envelope fields", ErrMaintenancePlanInvalid)
	}
	if !supportedPlan(plan.OperationType, plan.PayloadSchemaID) {
		return fmt.Errorf("%w: %w", ErrMaintenancePlanInvalid, ErrMaintenancePlanUnsupported)
	}
	return validatePlanActions(plan.Actions, true)
}

func validatePlanActions(actions []models.MaintenancePlanAction, requireSnapshots bool) error {
	if len(actions) == 0 {
		return fmt.Errorf("%w: no actions", ErrMaintenancePlanInvalid)
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if !planActionIDPattern.MatchString(action.ID) || strings.TrimSpace(action.Action) == "" || seen[action.ID] {
			return fmt.Errorf("%w: invalid or duplicate action %q", ErrMaintenancePlanInvalid, action.ID)
		}
		seen[action.ID] = true
		if len(action.BackupSources) == 0 {
			if requireSnapshots && (len(action.BackupFiles) != 0 || len(action.BackupDirectories) != 0 || action.BackupInventoryHash != "" || action.BackupFileCount != 0 || action.BackupTotalSize != 0) {
				return fmt.Errorf("%w: unexpected inventory for action %s", ErrMaintenancePlanInvalid, action.ID)
			}
			continue
		}
		if !validSourceList(action.BackupSources) {
			return fmt.Errorf("%w: invalid backup sources for action %s", ErrMaintenancePlanInvalid, action.ID)
		}
		if requireSnapshots {
			if !validBackupInventoryHash(action.BackupInventoryHash) || !validInventoryItems(action) {
				return fmt.Errorf("%w: malformed source snapshot for action %s", ErrMaintenancePlanInvalid, action.ID)
			}
			hash, err := repositories.ComputeBackupInventoryHash(action.BackupFiles, action.BackupDirectories)
			if err != nil || hash != action.BackupInventoryHash || action.BackupFileCount != int64(len(action.BackupFiles)) {
				return fmt.Errorf("%w: inconsistent source inventory for action %s", ErrMaintenancePlanInvalid, action.ID)
			}
			var total int64
			for _, file := range action.BackupFiles {
				total += file.Size
			}
			if total != action.BackupTotalSize {
				return fmt.Errorf("%w: inconsistent source size for action %s", ErrMaintenancePlanInvalid, action.ID)
			}
		}
	}
	return nil
}

func supportedPlan(operationType, schemaID string) bool {
	return supportedPlanSchemas[schemaID] == operationType
}

func planRequiresBackup(plan models.MaintenancePlanEnvelope, purpose string) bool {
	switch purpose {
	case "rehearsal":
		return (plan.OperationType == "migration" || plan.OperationType == "cleanup" || plan.OperationType == "legacy_ir_cutover") && (plan.BackupRequirement == "rehearsal" || plan.BackupRequirement == "rehearsal_and_apply")
	case "apply":
		return (plan.OperationType == "migration" || plan.OperationType == "migration_rollback" || plan.OperationType == "cleanup" || plan.OperationType == "legacy_ir_cutover") && (plan.BackupRequirement == "apply" || plan.BackupRequirement == "rehearsal_and_apply")
	default:
		return false
	}
}

func validBackupRequirement(value string) bool {
	switch value {
	case "none", "rehearsal", "apply", "rehearsal_and_apply":
		return true
	default:
		return false
	}
}

func validBackupRequirementForOperation(operationType, requirement string) bool {
	if !validBackupRequirement(requirement) {
		return false
	}
	switch operationType {
	case "migration":
		return requirement == "rehearsal_and_apply"
	case "legacy_ir_cutover":
		return requirement == "rehearsal_and_apply"
	case "migration_rollback":
		return requirement == "apply"
	case "cleanup":
		return requirement == "apply" || requirement == "rehearsal_and_apply"
	case "trash_final_delete":
		return requirement == "none"
	default:
		return false
	}
}

func validPlanDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validBackupInventoryHash(value string) bool { return validPlanDigest(value) }

func validInventoryItems(action models.MaintenancePlanAction) bool {
	paths := make(map[string]bool, len(action.BackupFiles)+len(action.BackupDirectories))
	var previous string
	for i, file := range action.BackupFiles {
		if !validSourcePath(file.Path) || file.Size < 0 || len(file.SHA256) != sha256.Size*2 || paths[file.Path] || (i > 0 && file.Path <= previous) {
			return false
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return false
		}
		switch file.JSONStatus {
		case "", "valid", "invalid", "empty":
		default:
			return false
		}
		if !sourceContainsInventoryPath(action.BackupSources, file.Path) {
			return false
		}
		paths[file.Path] = true
		previous = file.Path
	}
	previous = ""
	for i, directory := range action.BackupDirectories {
		if !validSourcePath(directory) || paths[directory] || (i > 0 && directory <= previous) || !sourceContainsInventoryPath(action.BackupSources, directory) {
			return false
		}
		paths[directory] = true
		previous = directory
	}
	return true
}

func sourceContainsInventoryPath(sources []string, item string) bool {
	for _, source := range sources {
		if backupPathContains(source, item) {
			return true
		}
	}
	return false
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON value")
	}
	encoded, err := json.Marshal(value)
	return encoded, err
}

func maintenancePlanDigest(plan models.MaintenancePlanEnvelope) (string, error) {
	plan.Digest = ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(encoded)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sameMaintenanceInventory(action models.MaintenancePlanAction, current repositories.BackupSourceInventory) bool {
	if action.BackupInventoryHash != current.InventoryHash || action.BackupFileCount != current.FileCount || action.BackupTotalSize != current.TotalSize || len(action.BackupFiles) != len(current.Files) || len(action.BackupDirectories) != len(current.Directories) {
		return false
	}
	for i := range action.BackupFiles {
		if action.BackupFiles[i] != current.Files[i] {
			return false
		}
	}
	for i := range action.BackupDirectories {
		if action.BackupDirectories[i] != current.Directories[i] {
			return false
		}
	}
	return true
}

func clearPlanActionInventory(action *models.MaintenancePlanAction) {
	action.BackupFiles = nil
	action.BackupDirectories = nil
	action.BackupInventoryHash = ""
	action.BackupFileCount = 0
	action.BackupTotalSize = 0
}

func cloneBackupFiles(files []models.BackupFile) []models.BackupFile {
	cloned := make([]models.BackupFile, len(files))
	copy(cloned, files)
	return cloned
}

func validateActionSelection(actionIDs []string) error {
	if len(actionIDs) == 0 {
		return ErrMaintenancePlanInvalid
	}
	seen := map[string]bool{}
	for _, id := range actionIDs {
		if !planActionIDPattern.MatchString(id) || seen[id] {
			return ErrMaintenancePlanInvalid
		}
		seen[id] = true
	}
	return nil
}

func validSourceList(sources []string) bool {
	if len(sources) == 0 {
		return false
	}
	for i, source := range sources {
		if !validSourcePath(source) {
			return false
		}
		for j, other := range sources {
			if i != j && backupPathContains(source, other) {
				return false
			}
		}
	}
	return true
}

func validSourcePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, `\:`) || strings.TrimSpace(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func backupPathContains(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func newMaintenancePlanID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "plan_" + hex.EncodeToString(random[:]), nil
}
