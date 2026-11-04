package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"backend/models"
	"backend/repositories"
)

var ErrMigrationVerifyInvalid = errors.New("invalid migration verification request or operation")

type migrationVerifyOperationStore interface {
	Load(string) (models.MaintenanceOperation, error)
}

type MigrationVerifyService struct {
	plans      *MigrationPlanService
	operations migrationVerifyOperationStore
	root       string
	inspector  MaintenanceFilesystemInspector
}

func NewMigrationVerifyService(plans *MigrationPlanService, operations *repositories.MaintenanceOperationRepository, root string) *MigrationVerifyService {
	return &MigrationVerifyService{plans: plans, operations: operations, root: root, inspector: MaintenanceFilesystemInspector{RuntimeRoot: root}}
}

// Verify is read-only. A failed target is reported in the result; malformed
// operation/plan identity or unavailable state is returned as an API error.
func (s *MigrationVerifyService) Verify(request models.MigrationVerifyRequest) (models.MigrationVerifyResult, error) {
	if s == nil || s.plans == nil || s.operations == nil || !filepath.IsAbs(s.root) || request.Validate() != nil {
		return models.MigrationVerifyResult{}, ErrMigrationVerifyInvalid
	}
	op, err := s.operations.Load(request.OperationID)
	if err != nil {
		return models.MigrationVerifyResult{}, err
	}
	if op.Type != "migration" || op.PlanID == "" || op.PlanDigest == "" || len(op.Actions) == 0 {
		return models.MigrationVerifyResult{}, ErrMigrationVerifyInvalid
	}
	plan, err := s.plans.Load(op.PlanID)
	if err != nil {
		return models.MigrationVerifyResult{}, err
	}
	if plan.Digest != op.PlanDigest {
		return models.MigrationVerifyResult{}, ErrMigrationVerifyInvalid
	}
	var payload models.MigrationPlanPayload
	if json.Unmarshal(plan.Payload, &payload) != nil {
		return models.MigrationVerifyResult{}, ErrMigrationVerifyInvalid
	}
	byID := map[string]models.MigrationPlanAction{}
	for _, a := range payload.Actions {
		byID[a.ActionID] = a
	}
	result := models.MigrationVerifyResult{OperationID: op.ID, PlanID: op.PlanID, Status: "verified", Targets: make([]models.MigrationVerifyTarget, 0, len(op.Actions))}
	for _, action := range op.Actions {
		item, ok := byID[action.ID]
		if !ok || item.Action != action.Action {
			return models.MigrationVerifyResult{}, ErrMigrationVerifyInvalid
		}
		v := s.verifyTarget(item, action)
		if v.Status != "verified" {
			result.Status = "failed"
		}
		result.Targets = append(result.Targets, v)
	}
	sort.Slice(result.Targets, func(i, j int) bool { return result.Targets[i].ActionID < result.Targets[j].ActionID })
	return result, nil
}

func (s *MigrationVerifyService) verifyTarget(item models.MigrationPlanAction, action models.MaintenanceOperationAction) models.MigrationVerifyTarget {
	v := models.MigrationVerifyTarget{ActionID: item.ActionID, TargetPath: item.TargetBefore.Path, Status: "failed"}
	fail := func(reason string) models.MigrationVerifyTarget { v.Reason = reason; return v }
	var snap models.MaintenanceSnapshot
	if json.Unmarshal(action.IntendedState, &snap) != nil || snap.Runtime == nil || snap.Runtime.Path != item.TargetBefore.Path || !snap.Runtime.Exists || snap.Runtime.Kind != "file" || !validMigrationHash(snap.Runtime.SHA256) {
		return fail("invalid_operation_snapshot")
	}
	v.ExpectedSHA256 = snap.Runtime.SHA256
	e, err := s.inspector.observe(s.root, item.TargetBefore.Path)
	if err != nil {
		return fail("unsafe_or_unreadable_target")
	}
	if !e.Exists {
		return fail("missing_target")
	}
	if e.Kind != "file" {
		return fail("invalid_target_type")
	}
	v.ActualSHA256 = e.SHA256
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(item.TargetBefore.Path)))
	if err != nil {
		return fail("unreadable_target")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != e.SHA256 {
		return fail("target_changed_during_read")
	}
	_, roomID, deviceID, targetID, ok := migrationTarget(item.TargetBefore.Path)
	if !ok {
		return fail("invalid_target_path")
	}
	repo := repositories.NewIRAdapterRepositoryWithRoomsDir(filepath.Join(s.root, "rooms"))
	var actual any
	if item.Action == "write_ir_command" {
		command, err := repo.LoadCommand(roomID, deviceID, targetID)
		if err != nil {
			return fail("repository_readback_failed")
		}
		actual = command
	} else {
		state, err := repo.LoadState(roomID, deviceID, targetID)
		if err != nil {
			return fail("repository_readback_failed")
		}
		actual = state
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var strict any
	if item.Action == "write_ir_command" {
		strict = new(models.IRCommand)
	} else {
		strict = new(models.IRState)
	}
	if decoder.Decode(strict) != nil || decoder.Decode(new(any)) != io.EOF {
		return fail("invalid_json_or_schema")
	}
	apply := &MigrationApplyService{inspector: s.inspector}
	signal, err := apply.readSource(item)
	if err != nil {
		return fail("source_changed")
	}
	expected, err := migrationExpectedValue(item, signal)
	if err != nil {
		return fail("invalid_source_or_plan")
	}
	comparison := compareMigrationValue(actual, expected)
	if comparison == "format_mismatch" || comparison == "count_mismatch" || comparison == "timings_or_captures_mismatch" {
		return fail(comparison)
	}
	if item.Action == "write_ir_command" {
		if validateIRCommand(actual.(models.IRCommand)) != nil {
			return fail("invalid_schema")
		}
	} else {
		if validateIRState(actual.(models.IRState)) != nil {
			return fail("invalid_schema")
		}
	}
	if err := apply.checkIdentity(item, signal.CarrierKHz, roomID, deviceID, false); err != nil {
		return fail("metadata_or_state_schema_mismatch")
	}
	for _, w := range item.PlannedWrites {
		if w.Role == "operation" {
			continue
		}
		observed, err := s.inspector.observe(s.root, w.Path)
		if err != nil || !observed.Exists || observed.Kind != map[bool]string{true: "directory", false: "file"}[w.Kind == "directory"] {
			return fail("missing_or_invalid_scaffold")
		}
		if w.Kind == "json" {
			body, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(w.Path)))
			if err != nil {
				return fail("unreadable_scaffold")
			}
			left, e1 := canonicalJSON(body)
			right, e2 := canonicalJSON(w.Content)
			if e1 != nil || e2 != nil || !bytes.Equal(left, right) {
				return fail("scaffold_changed")
			}
		}
	}
	if comparison != "" {
		return fail(comparison)
	}
	expectedBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return fail("invalid_source_or_plan")
	}
	expectedSum := sha256.Sum256(expectedBytes)
	if hex.EncodeToString(expectedSum[:]) != v.ExpectedSHA256 {
		return fail("invalid_operation_snapshot")
	}
	if v.ActualSHA256 != v.ExpectedSHA256 {
		return fail("hash_mismatch")
	}
	v.Status = "verified"
	return v
}

func compareMigrationValue(actual, expected any) string {
	switch a := actual.(type) {
	case models.IRCommand:
		e, ok := expected.(models.IRCommand)
		if !ok {
			return "invalid_operation_type"
		}
		if a.Format != e.Format {
			return "format_mismatch"
		}
		if a.Count != e.Count || len(a.Captures) != len(e.Captures) {
			return "count_mismatch"
		}
		if !reflect.DeepEqual(a.TimingsUS, e.TimingsUS) || !reflect.DeepEqual(a.Captures, e.Captures) {
			return "timings_or_captures_mismatch"
		}
		if !reflect.DeepEqual(a, e) {
			return "field_mismatch"
		}
	case models.IRState:
		e, ok := expected.(models.IRState)
		if !ok {
			return "invalid_operation_type"
		}
		if a.Format != e.Format {
			return "format_mismatch"
		}
		if a.Count != e.Count || len(a.Captures) != len(e.Captures) {
			return "count_mismatch"
		}
		if !reflect.DeepEqual(a.TimingsUS, e.TimingsUS) || !reflect.DeepEqual(a.Captures, e.Captures) {
			return "timings_or_captures_mismatch"
		}
		if !reflect.DeepEqual(a.State, e.State) {
			return "state_mismatch"
		}
		if !reflect.DeepEqual(a, e) {
			return "field_mismatch"
		}
	default:
		return "invalid_operation_type"
	}
	return ""
}
