package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"backend/models"
	"backend/repositories"
)

var ErrMigrationApplyInvalid = errors.New("invalid migration apply request")
var ErrMigrationApplyStale = errors.New("migration source or destination changed")

type migrationApplyBackup interface {
	CreateApply(string) (models.BackupDetail, error)
	Detail(string) (models.BackupDetail, error)
}

type MigrationApplyService struct {
	plans      *MigrationPlanService
	operations cleanupOperationStore
	backup     migrationApplyBackup
	writer     *repositories.MigrationApplyRepository
	inspector  MaintenanceFilesystemInspector
	newID      func(string) (string, error)
}

func NewMigrationApplyService(plans *MigrationPlanService, operations *repositories.MaintenanceOperationRepository, backup *BackupService, root string) *MigrationApplyService {
	return &MigrationApplyService{plans: plans, operations: operations, backup: backup, writer: repositories.NewMigrationApplyRepository(root), inspector: MaintenanceFilesystemInspector{RuntimeRoot: root}, newID: newCleanupID}
}

type migrationPrepared struct {
	item     models.MigrationPlanAction
	value    any
	before   models.MaintenanceSnapshot
	intended models.MaintenanceSnapshot
}

func (s *MigrationApplyService) Apply(request models.MigrationApplyRequest) (models.MaintenanceOperation, error) {
	if s == nil || s.plans == nil || s.plans.plans == nil || s.operations == nil || s.backup == nil || s.writer == nil || s.newID == nil || !filepath.IsAbs(s.inspector.RuntimeRoot) {
		return models.MaintenanceOperation{}, ErrMaintenancePlanNotReady
	}
	if err := request.Validate(); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrMigrationApplyInvalid, err)
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
	selection, err := s.plans.plans.VerifyApplySelection(plan.PlanID, plan.Digest, request.ActionIDs)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if plan.PayloadSchemaID == "migration.v2" && len(selection.ActionIDs) != len(bedroomPresetSpecs) {
		return models.MaintenanceOperation{}, ErrMigrationApplyInvalid
	}
	rehearsal, err := s.backup.Detail(request.RehearsalBackupID)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	if err := s.plans.plans.VerifyRehearsalBackup(plan.PlanID, plan.Digest, selection.ActionIDs, rehearsal, true); err != nil {
		return models.MaintenanceOperation{}, err
	}
	var payload models.MigrationPlanPayload
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		return models.MaintenanceOperation{}, errors.Join(ErrMigrationPlanInvalid, err)
	}
	byID := map[string]models.MigrationPlanAction{}
	for _, item := range payload.Actions {
		byID[item.ActionID] = item
	}
	prepared := make([]migrationPrepared, 0, len(selection.ActionIDs))
	for _, id := range selection.ActionIDs {
		item, ok := byID[id]
		if !ok {
			return models.MaintenanceOperation{}, ErrMaintenancePlanActionMissing
		}
		p, err := s.prepare(item)
		if err != nil {
			return models.MaintenanceOperation{}, err
		}
		prepared = append(prepared, p)
	}
	sort.Slice(prepared, func(i, j int) bool { return prepared[i].item.ActionID < prepared[j].item.ActionID })
	opID, err := s.newID("migration_")
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	created := map[string]string{}
	work := make([]MaintenanceActionWork, 0, len(prepared))
	for _, p := range prepared {
		p := p
		before, _ := json.Marshal(p.before)
		intended, _ := json.Marshal(p.intended)
		work = append(work, MaintenanceActionWork{ID: p.item.ActionID, Action: p.item.Action, BeforeState: before, IntendedState: intended,
			Mutate: func() error { return s.mutate(p, created) },
			Readback: func() (json.RawMessage, error) {
				e, err := s.inspector.observe(s.inspector.RuntimeRoot, p.item.TargetBefore.Path)
				if err != nil || e != *p.intended.Runtime {
					return nil, errors.Join(ErrMigrationApplyStale, err)
				}
				for _, w := range p.item.PlannedWrites {
					if hash, ok := created[w.Path]; ok {
						current, err := s.inspector.observe(s.inspector.RuntimeRoot, w.Path)
						if err != nil || !current.Exists || (w.Kind == "directory" && current.Kind != "directory") || (w.Kind != "directory" && (current.Kind != "file" || current.SHA256 != hash)) {
							return nil, errors.Join(ErrMigrationApplyStale, err)
						}
					}
				}
				return json.Marshal(p.intended)
			},
		})
	}
	draft := models.MaintenanceOperation{ID: opID, Target: "migration", Type: "migration", PlanID: plan.PlanID, PlanDigest: plan.Digest, RehearsalBackupID: request.RehearsalBackupID}
	if plan.PayloadSchemaID == "migration.v2" {
		for _, item := range prepared {
			draft.ClaimedPaths = append(draft.ClaimedPaths, item.item.TargetBefore.Path)
		}
	}
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
		if err := s.plans.plans.VerifyRehearsalBackup(plan.PlanID, plan.Digest, selection.ActionIDs, rehearsal, true); err != nil {
			return updated, err
		}
		for _, p := range prepared {
			if _, err := s.prepare(p.item); err != nil {
				return updated, err
			}
		}
		return updated, nil
	})
}

func (s *MigrationApplyService) prepare(item models.MigrationPlanAction) (migrationPrepared, error) {
	signal, err := s.readSource(item)
	if err != nil {
		return migrationPrepared{}, err
	}
	if err := s.checkDestination(item); err != nil {
		return migrationPrepared{}, err
	}
	_, roomID, deviceID, _, ok := migrationTarget(item.TargetBefore.Path)
	if !ok {
		return migrationPrepared{}, ErrMigrationPlanInvalid
	}
	value, err := migrationExpectedValue(item, signal)
	if err != nil {
		return migrationPrepared{}, err
	}
	if err := s.checkIdentity(item, signal.CarrierKHz, roomID, deviceID, true); err != nil {
		return migrationPrepared{}, err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return migrationPrepared{}, err
	}
	sum := sha256.Sum256(data)
	before := models.MaintenanceSnapshot{Runtime: &models.MaintenanceSnapshotEntry{Path: item.TargetBefore.Path}}
	intended := models.MaintenanceSnapshot{Runtime: &models.MaintenanceSnapshotEntry{Path: item.TargetBefore.Path, Exists: true, Kind: "file", SHA256: hex.EncodeToString(sum[:])}}
	return migrationPrepared{item: item, value: value, before: before, intended: intended}, nil
}

func migrationExpectedValue(item models.MigrationPlanAction, signal models.MigrationSourceSignal) (any, error) {
	_, _, _, targetID, ok := migrationTarget(item.TargetBefore.Path)
	if !ok || item.Decision == nil {
		return nil, ErrMigrationPlanInvalid
	}
	var value any
	if item.Action == "write_ir_command" {
		command := models.IRCommand{ID: targetID, Name: signal.Name, Format: item.TargetFormat, CarrierKHz: signal.CarrierKHz, Count: signal.Count, TimingsUS: signal.TimingsUS, Captures: signal.Captures}
		if item.TargetFormat == models.OperationFormatPresetCommand {
			command.Count = 0
			command.TimingsUS = nil
			command.PicoCommand = item.Decision.PicoCommand
		}
		if err := validateIRCommand(command); err != nil {
			return nil, errors.Join(ErrMigrationApplyInvalid, err)
		}
		value = command
	} else {
		state := models.IRState{ID: targetID, Name: signal.Name, State: item.Decision.State, Format: item.TargetFormat, CarrierKHz: signal.CarrierKHz, Count: signal.Count, TimingsUS: signal.TimingsUS, Captures: signal.Captures}
		if err := validateIRState(state); err != nil {
			return nil, errors.Join(ErrMigrationApplyInvalid, err)
		}
		value = state
	}
	if value == nil {
		return nil, ErrMigrationPlanInvalid
	}
	return value, nil
}

func (s *MigrationApplyService) readSource(item models.MigrationPlanAction) (models.MigrationSourceSignal, error) {
	if item.SourceKind == bedroomPresetSourceKind {
		if !validBedroomPresetItem(item) {
			return models.MigrationSourceSignal{}, ErrMigrationPlanInvalid
		}
		return models.MigrationSourceSignal{Name: item.Decision.TargetName, Room: "bedroom", Device: "roomlight", Format: "preset_command", CarrierKHz: 38}, nil
	}
	e, err := s.inspector.observe(s.inspector.RuntimeRoot, item.Source.Path)
	if err != nil || !e.Exists || e.Kind != "file" || e.SHA256 != item.Source.SHA256 {
		return models.MigrationSourceSignal{}, errors.Join(ErrMigrationApplyStale, err)
	}
	p := filepath.Join(s.inspector.RuntimeRoot, filepath.FromSlash(item.Source.Path))
	data, err := os.ReadFile(p)
	if err != nil || int64(len(data)) != item.Source.Size {
		return models.MigrationSourceSignal{}, errors.Join(ErrMigrationApplyStale, err)
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != item.Source.SHA256 {
		return models.MigrationSourceSignal{}, ErrMigrationApplyStale
	}
	var signal models.MigrationSourceSignal
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&signal) != nil || d.Decode(new(any)) != io.EOF || signal.Format != item.SourceFormat || signal.Name != strings.TrimSuffix(path.Base(item.Source.Path), ".json") || !validMigrationSignal(signal) {
		return models.MigrationSourceSignal{}, ErrMigrationApplyStale
	}
	return signal, nil
}

func (s *MigrationApplyService) checkDestination(item models.MigrationPlanAction) error {
	e, err := s.inspector.observe(s.inspector.RuntimeRoot, item.TargetBefore.Path)
	if err != nil || e.Exists {
		return errors.Join(ErrMigrationApplyStale, err)
	}
	parent, err := s.inspector.observe(s.inspector.RuntimeRoot, item.TargetParentBefore.Path)
	if err != nil || parent.Exists != item.TargetParentBefore.Exists || parent.Kind != item.TargetParentBefore.Kind || parent.SHA256 != item.TargetParentBefore.SHA256 {
		return errors.Join(ErrMigrationApplyStale, err)
	}
	for _, w := range item.PlannedWrites {
		e, err := s.inspector.observe(s.inspector.RuntimeRoot, w.Path)
		if err != nil || e.Exists {
			return errors.Join(ErrMigrationApplyStale, err)
		}
	}
	return nil
}

func (s *MigrationApplyService) checkIdentity(item models.MigrationPlanAction, carrier int, roomID, deviceID string, usePlanned bool) error {
	writes := map[string]json.RawMessage{}
	for _, w := range item.PlannedWrites {
		if usePlanned && w.Kind == "json" && w.Role != "operation" {
			writes[w.Path] = w.Content
		}
	}
	read := func(rel string, out any) error {
		if data, ok := writes[rel]; ok {
			if json.Unmarshal(data, out) != nil {
				return ErrMigrationPlanInvalid
			}
			return nil
		}
		e, err := s.inspector.observe(s.inspector.RuntimeRoot, rel)
		if err != nil || !e.Exists || e.Kind != "file" {
			return errors.Join(ErrMigrationApplyStale, err)
		}
		data, err := os.ReadFile(filepath.Join(s.inspector.RuntimeRoot, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return ErrMigrationApplyStale
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
			return ErrMigrationApplyStale
		}
		return nil
	}
	base := path.Join("rooms", roomID)
	var room models.Room
	if err := read(path.Join(base, "room.json"), &room); err != nil {
		return err
	}
	var device models.Device
	if err := read(path.Join(base, "devices", deviceID, "device.json"), &device); err != nil {
		return err
	}
	irBase := path.Join(base, "devices", deviceID, "adapters", "ir")
	var config models.IRConfig
	if err := read(path.Join(irBase, "ir_config.json"), &config); err != nil {
		return err
	}
	expected := models.IRRemoteTypeCommand
	if item.Action == "write_ir_state" {
		expected = models.IRRemoteTypeStateful
	}
	if room.ID != roomID || strings.TrimSpace(room.Name) == "" || device.ID != deviceID || device.RoomID != roomID || device.PrimaryAdapter != "ir" || device.DeviceType == "" || config.IRRemoteType != expected || config.CarrierKHz != carrier {
		return ErrMigrationApplyStale
	}
	if item.SourceKind == bedroomPresetSourceKind && (!reflect.DeepEqual(room, models.Room{ID: "bedroom", Name: "Bedroom"}) || !reflect.DeepEqual(device, models.Device{ID: "roomlight", Name: "Roomlight", RoomID: "bedroom", DeviceType: "light", PrimaryAdapter: "ir"}) || !reflect.DeepEqual(config, models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38, DefaultRepeat: 1})) {
		return ErrMigrationApplyStale
	}
	if item.Action == "write_ir_state" {
		var schema models.IRStateSchema
		if err := read(path.Join(irBase, "state_schema.json"), &schema); err != nil {
			return err
		}
		if validateIRStateSchema(schema) != nil || validateIRStateMatchesSchema(item.Decision.State, schema) != nil {
			return ErrMigrationApplyStale
		}
	}
	return nil
}

func (s *MigrationApplyService) mutate(p migrationPrepared, created map[string]string) error {
	if _, err := s.readSource(p.item); err != nil {
		return err
	}
	if e, err := s.inspector.observe(s.inspector.RuntimeRoot, p.item.TargetBefore.Path); err != nil || e.Exists {
		return errors.Join(ErrMigrationApplyStale, err)
	}
	writes := append([]models.MigrationPlannedWrite(nil), p.item.PlannedWrites...)
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].Kind != writes[j].Kind {
			return writes[i].Kind == "directory"
		}
		if writes[i].Role == "operation" {
			return false
		}
		if writes[j].Role == "operation" {
			return true
		}
		return len(writes[i].Path) < len(writes[j].Path) || len(writes[i].Path) == len(writes[j].Path) && writes[i].Path < writes[j].Path
	})
	for _, w := range writes {
		if hash, ok := created[w.Path]; ok {
			e, err := s.inspector.observe(s.inspector.RuntimeRoot, w.Path)
			if err != nil || !e.Exists || (w.Kind == "directory" && e.Kind != "directory") || (w.Kind != "directory" && (e.Kind != "file" || e.SHA256 != hash)) {
				return errors.Join(ErrMigrationApplyStale, err)
			}
			continue
		}
		e, err := s.inspector.observe(s.inspector.RuntimeRoot, w.Path)
		if err != nil || e.Exists {
			return errors.Join(ErrMigrationApplyStale, err)
		}
		if w.Kind == "directory" {
			err = s.writer.CreateDirectory(w.Path)
		} else if w.Role == "operation" {
			err = s.writer.CreateJSON(w.Path, p.value)
		} else {
			err = s.writer.CreateJSON(w.Path, w.Content)
		}
		if err != nil {
			return err
		}
		after, err := s.inspector.observe(s.inspector.RuntimeRoot, w.Path)
		if err != nil || !after.Exists {
			return errors.Join(ErrMaintenanceOutcomeUncertain, err)
		}
		created[w.Path] = after.SHA256
	}
	return nil
}
