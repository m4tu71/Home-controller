package services

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"sort"
	"strings"

	iradapter "backend/adapters/ir"
	"backend/models"
)

var ErrMigrationPlanInvalid = errors.New("invalid migration plan payload")

type MigrationPlanService struct{ plans *MaintenancePlanService }

func NewMigrationPlanService(plans *MaintenancePlanService) *MigrationPlanService {
	return &MigrationPlanService{plans: plans}
}

// Create accepts already observed snapshots. Step 4A will own filesystem
// inspection and HTTP mapping; this layer only validates and seals the schema.
func (s *MigrationPlanService) Create(payload models.MigrationPlanPayload) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrMigrationPlanInvalid
	}
	if err := validateMigrationPayload(payload); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if s.plans.backups == nil {
		return models.MaintenancePlanEnvelope{}, ErrMaintenancePlanInvalid
	}
	builtin := payload.Actions[0].SourceKind == bedroomPresetSourceKind
	for _, item := range payload.Actions {
		source := migrationBackupSource(item)
		inventory, err := s.plans.backups.InspectSources([]string{source})
		if err != nil || !builtin && (len(inventory.Files) != 1 || inventory.Files[0].Path != item.Source.Path || inventory.Files[0].Size != item.Source.Size || inventory.Files[0].SHA256 != item.Source.SHA256) || builtin && !containsString(inventory.Directories, "rooms") {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMaintenancePlanStale, err)
		}
	}
	payload.Actions = append([]models.MigrationPlanAction(nil), payload.Actions...)
	for i := range payload.Actions {
		payload.Actions[i].PlannedWrites = append([]models.MigrationPlannedWrite(nil), payload.Actions[i].PlannedWrites...)
		sort.Slice(payload.Actions[i].PlannedWrites, func(a, b int) bool {
			return payload.Actions[i].PlannedWrites[a].Path < payload.Actions[i].PlannedWrites[b].Path
		})
	}
	actions := make([]models.MaintenancePlanAction, 0, len(payload.Actions))
	ready := true
	for _, item := range payload.Actions {
		selectable := migrationDecisionComplete(item)
		if !selectable {
			ready = false
		}
		actions = append(actions, models.MaintenancePlanAction{ID: item.ActionID, Action: item.Action, Selectable: selectable, Destructive: false, BackupSources: []string{migrationBackupSource(item)}})
	}
	sort.Slice(payload.Actions, func(i, j int) bool { return payload.Actions[i].ActionID < payload.Actions[j].ActionID })
	sort.Slice(actions, func(i, j int) bool { return actions[i].ID < actions[j].ID })
	encoded, err := json.Marshal(payload)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	schema := "migration.v1"
	if builtin {
		schema = "migration.v2"
	}
	draft := models.MaintenancePlanEnvelope{OperationType: "migration", PayloadSchemaID: schema, Payload: encoded, ReadyToApply: ready, BackupRequirement: "rehearsal_and_apply", Actions: actions}
	if err := ValidateMigrationPlan(draft); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return s.plans.Create(draft)
}

func (s *MigrationPlanService) Load(planID string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.plans == nil {
		return models.MaintenancePlanEnvelope{}, ErrMigrationPlanInvalid
	}
	plan, err := s.plans.Load(planID)
	if err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	if err := ValidateMigrationPlan(plan); err != nil {
		return models.MaintenancePlanEnvelope{}, err
	}
	return plan, nil
}

func ValidateMigrationPlan(plan models.MaintenancePlanEnvelope) error {
	if plan.OperationType != "migration" || (plan.PayloadSchemaID != "migration.v1" && plan.PayloadSchemaID != "migration.v2") || plan.BackupRequirement != "rehearsal_and_apply" || len(plan.Actions) == 0 {
		return ErrMigrationPlanInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(plan.Payload))
	decoder.DisallowUnknownFields()
	var payload models.MigrationPlanPayload
	if err := decoder.Decode(&payload); err != nil {
		return errors.Join(ErrMigrationPlanInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrMigrationPlanInvalid
	}
	if err := validateMigrationPayload(payload); err != nil {
		return err
	}
	if (plan.PayloadSchemaID == "migration.v2") != (payload.Actions[0].SourceKind == bedroomPresetSourceKind) {
		return ErrMigrationPlanInvalid
	}
	if len(plan.Actions) != len(payload.Actions) {
		return ErrMigrationPlanInvalid
	}
	byID := make(map[string]models.MaintenancePlanAction, len(plan.Actions))
	for _, action := range plan.Actions {
		if _, exists := byID[action.ID]; exists {
			return ErrMigrationPlanInvalid
		}
		byID[action.ID] = action
	}
	ready := true
	for _, item := range payload.Actions {
		action, ok := byID[item.ActionID]
		selectable := migrationDecisionComplete(item)
		if !selectable {
			ready = false
		}
		if !ok || action.Action != item.Action || action.Selectable != selectable || action.Destructive || len(action.BackupSources) != 1 || action.BackupSources[0] != migrationBackupSource(item) {
			return ErrMigrationPlanInvalid
		}
	}
	if plan.ReadyToApply != ready {
		return ErrMigrationPlanInvalid
	}
	return nil
}

func validateMigrationPayload(payload models.MigrationPlanPayload) error {
	if len(payload.Actions) == 0 {
		return ErrMigrationPlanInvalid
	}
	builtin := payload.Actions[0].SourceKind == bedroomPresetSourceKind
	if builtin && len(payload.Actions) != len(bedroomPresetSpecs) {
		return ErrMigrationPlanInvalid
	}
	ids, targets := map[string]bool{}, map[string]bool{}
	sharedWrites := map[string]models.MigrationPlannedWrite{}
	for _, item := range payload.Actions {
		if !planActionIDPattern.MatchString(item.ActionID) || ids[item.ActionID] || !validMigrationTarget(item.TargetBefore, item.TargetParentBefore) || targets[item.TargetBefore.Path] {
			return ErrMigrationPlanInvalid
		}
		if builtin {
			if !validBedroomPresetItem(item) {
				return ErrMigrationPlanInvalid
			}
		} else if item.SourceKind != "" || !validMigrationSource(item.Source) || item.Source.Path == item.TargetBefore.Path || item.ExpectedAfterState != "target_created_source_preserved" {
			return ErrMigrationPlanInvalid
		}
		if err := validateMigrationWrites(item); err != nil {
			return err
		}
		for _, write := range item.PlannedWrites {
			if previous, exists := sharedWrites[write.Path]; exists && !sameMigrationWrite(previous, write) {
				return ErrMigrationPlanInvalid
			}
			sharedWrites[write.Path] = write
		}
		ids[item.ActionID], targets[item.TargetBefore.Path] = true, true
		kind, roomID, deviceID, targetID, ok := migrationTarget(item.TargetBefore.Path)
		if !ok || (kind == "command" && item.Action != "write_ir_command") || (kind == "state" && item.Action != "write_ir_state") {
			return ErrMigrationPlanInvalid
		}
		if !validMigrationFormat(item, kind) {
			return ErrMigrationPlanInvalid
		}
		if item.Decision != nil {
			if !builtin && item.Decision.TargetName != "" {
				return ErrMigrationPlanInvalid
			}
			if item.SourceFormat == "preset_command" && item.Decision.PicoCommand != "" && !iradapter.IsAllowedPresetCommand(item.Decision.PicoCommand) {
				return ErrMigrationPlanInvalid
			}
			if item.SourceFormat != "preset_command" && item.Decision.PicoCommand != "" {
				return ErrMigrationPlanInvalid
			}
			if item.Decision.RoomID != "" && item.Decision.RoomID != roomID || item.Decision.DeviceID != "" && item.Decision.DeviceID != deviceID || item.Decision.TargetID != "" && item.Decision.TargetID != targetID {
				return ErrMigrationPlanInvalid
			}
			if kind == "command" && len(item.Decision.State) != 0 {
				return ErrMigrationPlanInvalid
			}
			if kind == "state" && item.Decision.PicoCommand != "" {
				return ErrMigrationPlanInvalid
			}
			if item.SourceFormat != "raw_median" && item.Decision.ApproveRawMedian {
				return ErrMigrationPlanInvalid
			}
		}
	}
	if builtin {
		for _, spec := range bedroomPresetSpecs {
			if !targets[path.Join("rooms", "bedroom", "devices", "roomlight", "adapters", "ir", "commands", spec.ID+".json")] {
				return ErrMigrationPlanInvalid
			}
		}
	}
	return nil
}

func migrationBackupSource(item models.MigrationPlanAction) string {
	if item.SourceKind == bedroomPresetSourceKind {
		return "rooms"
	}
	return item.Source.Path
}

func sameMigrationWrite(a, b models.MigrationPlannedWrite) bool {
	if a.Path != b.Path || a.Kind != b.Kind || a.Role != b.Role || a.Before != b.Before || a.ExpectedAfterState != b.ExpectedAfterState {
		return false
	}
	left, err1 := canonicalJSON(a.Content)
	right, err2 := canonicalJSON(b.Content)
	if len(a.Content) == 0 && len(b.Content) == 0 {
		return true
	}
	return err1 == nil && err2 == nil && bytes.Equal(left, right)
}

func validMigrationSource(v models.MigrationPathSnapshot) bool {
	return validSourcePath(v.Path) && v.Exists && v.Kind == "file" && v.Size >= 0 && validMigrationHash(v.SHA256)
}

func validMigrationTarget(v, parent models.MigrationPathSnapshot) bool {
	if !validSourcePath(v.Path) || v.Exists || v.Kind != "" || v.Size != 0 || v.SHA256 != "" || !validSourcePath(parent.Path) || parent.Path != path.Dir(v.Path) {
		return false
	}
	if parent.Exists {
		return parent.Kind == "directory" && parent.Size >= 0 && validMigrationHash(parent.SHA256)
	}
	return parent.Kind == "" && parent.Size == 0 && parent.SHA256 == ""
}

func validateMigrationWrites(item models.MigrationPlanAction) error {
	if len(item.PlannedWrites) == 0 {
		return ErrMigrationPlanInvalid
	}
	seen := map[string]bool{}
	targetWrite, parentWrite := false, false
	for _, write := range item.PlannedWrites {
		if !validSourcePath(write.Path) || seen[write.Path] || write.Before.Path != write.Path || write.Before.Exists || write.Before.Kind != "" || write.Before.Size != 0 || write.Before.SHA256 != "" || write.ExpectedAfterState != "created" || write.Path == item.Source.Path {
			return ErrMigrationPlanInvalid
		}
		seen[write.Path] = true
		if write.Path == item.TargetBefore.Path {
			if write.Kind != "json" || write.Role != "operation" || len(write.Content) != 0 || write.Before != item.TargetBefore {
				return ErrMigrationPlanInvalid
			}
			targetWrite = true
			continue
		}
		switch write.Kind {
		case "directory":
			if !strings.HasPrefix(item.TargetBefore.Path, write.Path+"/") {
				return ErrMigrationPlanInvalid
			}
			if write.Role != "directory" || len(write.Content) != 0 {
				return ErrMigrationPlanInvalid
			}
			if write.Path == item.TargetParentBefore.Path {
				parentWrite = true
			}
		case "json":
			if !validMigrationScaffoldJSONPath(write.Path, write.Role, item.TargetBefore.Path) || len(write.Content) > 4096 || !validMigrationScaffoldContent(write) {
				return ErrMigrationPlanInvalid
			}
		default:
			return ErrMigrationPlanInvalid
		}
	}
	if !targetWrite || (!item.TargetParentBefore.Exists && !parentWrite) {
		return ErrMigrationPlanInvalid
	}
	return nil
}

func validMigrationScaffoldContent(write models.MigrationPlannedWrite) bool {
	if !validJSONObject(write.Content) {
		return false
	}
	decode := func(value any) bool {
		decoder := json.NewDecoder(bytes.NewReader(write.Content))
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) != nil {
			return false
		}
		var trailing any
		return decoder.Decode(&trailing) == io.EOF
	}
	parts := strings.Split(write.Path, "/")
	switch write.Role {
	case "room":
		var room models.Room
		return decode(&room) && room.ID == parts[1] && strings.TrimSpace(room.Name) != ""
	case "device":
		var device models.Device
		return decode(&device) && device.ID == parts[3] && device.RoomID == parts[1] && strings.TrimSpace(device.Name) != "" && device.DeviceType != "" && device.PrimaryAdapter != ""
	case "ir_config":
		var config models.IRConfig
		return decode(&config) && (config.IRRemoteType == models.IRRemoteTypeCommand || config.IRRemoteType == models.IRRemoteTypeStateful) && config.CarrierKHz > 0
	case "state_schema":
		var schema models.IRStateSchema
		return decode(&schema) && validateIRStateSchema(schema) == nil
	default:
		return false
	}
}

func validMigrationScaffoldJSONPath(value, role, target string) bool {
	parts := strings.Split(value, "/")
	targetParts := strings.Split(target, "/")
	if len(targetParts) != 8 || parts[0] != "rooms" || len(parts) < 2 || parts[1] != targetParts[1] {
		return false
	}
	switch role {
	case "room":
		return len(parts) == 3 && parts[0] == "rooms" && parts[2] == "room.json"
	case "device":
		return len(parts) == 5 && parts[2] == "devices" && parts[3] == targetParts[3] && parts[4] == "device.json"
	case "ir_config":
		return len(parts) == 7 && parts[2] == "devices" && parts[3] == targetParts[3] && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "ir_config.json"
	case "state_schema":
		return len(parts) == 7 && parts[2] == "devices" && parts[3] == targetParts[3] && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "state_schema.json" && targetParts[6] == "states"
	default:
		return false
	}
}

func validMigrationHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func migrationTarget(target string) (kind, roomID, deviceID, targetID string, ok bool) {
	parts := strings.Split(target, "/")
	if len(parts) != 8 || parts[0] != "rooms" || parts[2] != "devices" || parts[4] != "adapters" || parts[5] != "ir" || !strings.HasSuffix(parts[7], ".json") || len(parts[7]) <= len(".json") {
		return
	}
	switch parts[6] {
	case "commands":
		kind = "command"
	case "states":
		kind = "state"
	default:
		return
	}
	return kind, parts[1], parts[3], strings.TrimSuffix(parts[7], ".json"), true
}

func validMigrationFormat(item models.MigrationPlanAction, kind string) bool {
	switch item.SourceFormat {
	case "raw_samples":
		return item.TargetFormat == models.OperationFormatRawSamples && item.Transform == "preserve_raw_samples"
	case "raw":
		return item.TargetFormat == models.OperationFormatRaw && item.Transform == "preserve_raw"
	case "raw_median":
		return item.TargetFormat == models.OperationFormatRaw && item.Transform == "raw_median_to_raw"
	case "preset_command":
		return kind == "command" && item.TargetFormat == models.OperationFormatPresetCommand && item.Transform == "preserve_preset_command"
	default:
		return false
	}
}

func migrationDecisionComplete(item models.MigrationPlanAction) bool {
	if item.SourceKind == bedroomPresetSourceKind {
		return validBedroomPresetItem(item)
	}
	decision := item.Decision
	if decision == nil {
		return false
	}
	kind, roomID, deviceID, targetID, ok := migrationTarget(item.TargetBefore.Path)
	if !ok || decision.RoomID != roomID || decision.DeviceID != deviceID || decision.TargetID != targetID {
		return false
	}
	if kind == "state" && len(decision.State) == 0 {
		return false
	}
	if item.SourceFormat == "raw_median" && !decision.ApproveRawMedian {
		return false
	}
	if item.SourceFormat == "preset_command" && strings.TrimSpace(decision.PicoCommand) == "" {
		return false
	}
	return true
}
