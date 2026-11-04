package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"backend/models"
)

var (
	ErrCleanupSelectionInvalid = errors.New("invalid cleanup issue selection")
	ErrCleanupIssueMissing     = errors.New("cleanup issue not found")
	ErrCleanupIssueStale       = errors.New("cleanup issue changed")
	ErrCleanupIssueUnsupported = errors.New("issue is not a cleanup target")
)

type CleanupPlanService struct {
	health *DataHealthService
	plans  *MaintenancePlanService
}

func NewCleanupPlanService(health *DataHealthService, plans *MaintenancePlanService) *CleanupPlanService {
	return &CleanupPlanService{health: health, plans: plans}
}

// Create stores only explicitly selected, still-current issues. It does not
// mutate runtime data; source inventories are sealed by the common plan layer.
func (s *CleanupPlanService) Create(selected []models.CleanupIssueSelection) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.health == nil || s.health.Repository == nil || s.plans == nil || len(selected) == 0 {
		return models.MaintenancePlanEnvelope{}, ErrCleanupSelectionInvalid
	}
	root := s.health.Repository.Root
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return models.MaintenancePlanEnvelope{}, ErrCleanupSelectionInvalid
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return models.MaintenancePlanEnvelope{}, ErrCleanupSelectionInvalid
	}
	issues, err := s.health.Scan()
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	byID := make(map[string]models.DataHealthIssue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	seen := make(map[string]bool, len(selected))
	paths := make([]string, 0, len(selected))
	var payload models.CleanupPlanPayload
	var actions []models.MaintenancePlanAction
	for _, item := range selected {
		if item.IssueID == "" || !validCleanupHash(item.BeforeHash) || seen[item.IssueID] {
			return models.MaintenancePlanEnvelope{}, ErrCleanupSelectionInvalid
		}
		seen[item.IssueID] = true
		issue, ok := byID[item.IssueID]
		if !ok {
			return models.MaintenancePlanEnvelope{}, ErrCleanupIssueMissing
		}
		if issue.Hash != item.BeforeHash {
			return models.MaintenancePlanEnvelope{}, ErrCleanupIssueStale
		}
		action, rel, err := cleanupActionForIssue(root, issue)
		if err != nil {
			return models.MaintenancePlanEnvelope{}, err
		}
		for _, previous := range paths {
			if rel == previous || strings.HasPrefix(rel, previous+"/") || strings.HasPrefix(previous, rel+"/") {
				return models.MaintenancePlanEnvelope{}, ErrCleanupSelectionInvalid
			}
		}
		paths = append(paths, rel)
		payload.Actions = append(payload.Actions, action)
		actions = append(actions, models.MaintenancePlanAction{ID: action.ActionID, Action: action.Action, Selectable: true, Destructive: true, BackupSources: []string{rel}})
	}
	sort.Slice(payload.Actions, func(i, j int) bool { return payload.Actions[i].ActionID < payload.Actions[j].ActionID })
	sort.Slice(actions, func(i, j int) bool { return actions[i].ID < actions[j].ID })
	encoded, err := json.Marshal(payload)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	draft := models.MaintenancePlanEnvelope{OperationType: "cleanup", PayloadSchemaID: "cleanup.v1", Payload: encoded, ReadyToApply: true, BackupRequirement: "apply", Actions: actions}
	if err := ValidateCleanupPlan(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return s.plans.Create(draft)
}

func (s *CleanupPlanService) Load(planID string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	plan, err := s.plans.Load(planID)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if err := ValidateCleanupPlan(plan); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return plan, nil
}

// ValidateCleanupPlan checks cleanup-specific payload and envelope agreement;
// the shared plan service separately validates envelope identity and digest.
func ValidateCleanupPlan(plan models.MaintenancePlanEnvelope) error {
	if plan.OperationType != "cleanup" || plan.PayloadSchemaID != "cleanup.v1" || !plan.ReadyToApply || plan.BackupRequirement != "apply" || len(plan.Actions) == 0 {
		return ErrMaintenancePlanInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(plan.Payload))
	decoder.DisallowUnknownFields()
	var payload models.CleanupPlanPayload
	if err := decoder.Decode(&payload); err != nil {
		return errors.Join(ErrMaintenancePlanInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrMaintenancePlanInvalid
	}
	if len(payload.Actions) != len(plan.Actions) {
		return ErrMaintenancePlanInvalid
	}
	byID := make(map[string]models.MaintenancePlanAction, len(plan.Actions))
	for _, action := range plan.Actions {
		byID[action.ID] = action
	}
	seenIssues, seenPaths := map[string]bool{}, map[string]bool{}
	for _, item := range payload.Actions {
		action, ok := byID[item.ActionID]
		if !ok || seenIssues[item.IssueID] || len(item.AffectedPaths) != 1 || !validCleanupHash(item.BeforeHash) || item.ActionID != cleanupActionID(item.IssueID) || !item.Destructive || item.ExpectedAfterState != "moved_to_trash" || !action.Selectable || !action.Destructive || action.Action != item.Action || len(action.BackupSources) != 1 || action.BackupSources[0] != item.AffectedPaths[0] {
			return ErrMaintenancePlanInvalid
		}
		kind, rel, ok := strings.Cut(item.IssueID, ":")
		if !ok || !validCleanupRelative(rel) || rel != item.AffectedPaths[0] || cleanupAction(kind, rel) != item.Action || seenPaths[rel] {
			return ErrMaintenancePlanInvalid
		}
		seenIssues[item.IssueID], seenPaths[rel] = true, true
	}
	for rel := range seenPaths {
		for other := range seenPaths {
			if rel != other && strings.HasPrefix(other, rel+"/") {
				return ErrMaintenancePlanInvalid
			}
		}
	}
	return nil
}

func cleanupActionForIssue(root string, issue models.DataHealthIssue) (models.CleanupPlanAction, string, error) {
	kind, rel, ok := strings.Cut(issue.ID, ":")
	if !ok || !validCleanupRelative(rel) || issue.Path != "data/"+rel || issue.CanonicalPath != filepath.Join(root, filepath.FromSlash(rel)) || !validCleanupHash(issue.Hash) {
		return models.CleanupPlanAction{}, "", ErrCleanupSelectionInvalid
	}
	action := cleanupAction(kind, rel)
	if action == "" || !issue.HasDestructiveActions {
		return models.CleanupPlanAction{}, "", ErrCleanupIssueUnsupported
	}
	current := root
	var targetInfo os.FileInfo
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return models.CleanupPlanAction{}, "", errors.Join(ErrCleanupIssueStale, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return models.CleanupPlanAction{}, "", ErrCleanupIssueUnsupported
		}
		targetInfo = info
	}
	if (action == "trash_directory" && !targetInfo.IsDir()) || (action == "trash_file" && !targetInfo.Mode().IsRegular()) {
		return models.CleanupPlanAction{}, "", ErrCleanupIssueStale
	}
	return models.CleanupPlanAction{IssueID: issue.ID, ActionID: cleanupActionID(issue.ID), Action: action, Destructive: true, AffectedPaths: []string{rel}, BeforeHash: issue.Hash, ExpectedAfterState: "moved_to_trash"}, rel, nil
}

func cleanupAction(kind, rel string) string {
	parts := strings.Split(rel, "/")
	switch kind {
	case "missing_room_json", "orphan_room":
		if len(parts) == 2 && parts[0] == "rooms" {
			return "trash_directory"
		}
	case "missing_device_json", "orphan_device":
		if len(parts) == 4 && parts[0] == "rooms" && parts[2] == "devices" {
			return "trash_directory"
		}
	case "orphan_adapter":
		if len(parts) == 6 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" {
			return "trash_directory"
		}
	case "empty_directory":
		if (len(parts) == 2 && parts[0] == "rooms") || (len(parts) == 4 && parts[0] == "rooms" && parts[2] == "devices") || (len(parts) == 6 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters") {
			return "trash_directory"
		}
	case "empty_json", "invalid_json", "unsupported_format":
		legacySignal := len(parts) == 2 && parts[0] == "ir-signals" && strings.HasSuffix(parts[1], ".json")
		irOperation := len(parts) == 8 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && (parts[6] == "commands" || parts[6] == "states") && strings.HasSuffix(parts[7], ".json")
		knownJSON := (len(parts) == 3 && parts[0] == "rooms" && parts[2] == "room.json") || (len(parts) == 5 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "device.json") || (len(parts) == 7 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && (parts[6] == "ir_config.json" || parts[6] == "state_schema.json"))
		if (kind == "unsupported_format" && (legacySignal || irOperation)) || (kind != "unsupported_format" && (legacySignal || irOperation || knownJSON)) {
			return "trash_file"
		}
	}
	return ""
}

func validCleanupRelative(rel string) bool {
	if rel == "" || strings.Contains(rel, "\\") || strings.HasPrefix(rel, "/") || filepath.ToSlash(filepath.Clean(rel)) != rel {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validCleanupHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cleanupActionID(issueID string) string {
	sum := sha256.Sum256([]byte(issueID))
	return fmt.Sprintf("cleanup_%x", sum[:])
}
