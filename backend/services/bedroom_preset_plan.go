package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"backend/models"
)

var ErrBedroomPresetUnavailable = errors.New("bedroom preset destination is not empty or safe")

// BedroomPresetPlanService seals the six approved firmware presets without
// inventing legacy IR Signal files. The existing rooms subtree is the backup
// anchor; if it does not exist, this plan fails closed.
type BedroomPresetPlanService struct {
	Root      string
	Migration *MigrationPlanService
}

func NewBedroomPresetPlanService(root string, migration *MigrationPlanService) *BedroomPresetPlanService {
	return &BedroomPresetPlanService{Root: root, Migration: migration}
}

func (s *BedroomPresetPlanService) Create() (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.Migration == nil || !filepath.IsAbs(s.Root) {
		return models.MaintenancePlanEnvelope{}, ErrBedroomPresetUnavailable
	}
	inspector := MaintenanceFilesystemInspector{}
	anchor, err := inspector.observe(s.Root, "rooms")
	if err != nil || !anchor.Exists || anchor.Kind != "directory" {
		return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
	}
	bedroom, err := inspector.observe(s.Root, "rooms/bedroom")
	if err != nil || bedroom.Exists && bedroom.Kind != "directory" {
		return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
	}
	room := models.Room{ID: "bedroom", Name: "Bedroom"}
	device := models.Device{ID: "roomlight", Name: "Roomlight", RoomID: "bedroom", DeviceType: "light", PrimaryAdapter: "ir"}
	config := models.IRConfig{IRRemoteType: models.IRRemoteTypeCommand, CarrierKHz: 38, DefaultRepeat: 1}
	base := "rooms/bedroom/devices/roomlight/adapters/ir"
	parent := path.Join(base, "commands")
	dirs := []string{"rooms/bedroom", "rooms/bedroom/devices", "rooms/bedroom/devices/roomlight", "rooms/bedroom/devices/roomlight/adapters", base, parent}
	meta := []struct {
		rel, role string
		value     any
	}{
		{"rooms/bedroom/room.json", "room", room},
		{"rooms/bedroom/devices/roomlight/device.json", "device", device},
		{path.Join(base, "ir_config.json"), "ir_config", config},
	}
	if bedroom.Exists {
		for _, dir := range dirs {
			e, err := inspector.observe(s.Root, dir)
			if err != nil || !e.Exists || e.Kind != "directory" {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
		}
		for _, m := range meta {
			e, err := inspector.observe(s.Root, m.rel)
			if err != nil || !e.Exists || e.Kind != "file" {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
			data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(m.rel)))
			if err != nil {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
			var actual any
			switch m.value.(type) {
			case models.Room:
				actual = new(models.Room)
			case models.Device:
				actual = new(models.Device)
			case models.IRConfig:
				actual = new(models.IRConfig)
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if dec.Decode(actual) != nil || dec.Decode(new(any)) != io.EOF {
				return models.MaintenancePlanEnvelope{}, ErrBedroomPresetUnavailable
			}
			value, _ := json.Marshal(actual)
			expected, _ := json.Marshal(m.value)
			if !bytes.Equal(value, expected) {
				return models.MaintenancePlanEnvelope{}, ErrBedroomPresetUnavailable
			}
		}
	}
	items := make([]models.MigrationPlanAction, 0, len(bedroomPresetSpecs))
	for _, spec := range bedroomPresetSpecs {
		target := path.Join(parent, spec.ID+".json")
		writes := []models.MigrationPlannedWrite{{Path: target, Kind: "json", Role: "operation", Before: models.MigrationPathSnapshot{Path: target}, ExpectedAfterState: "created"}}
		for _, dir := range dirs {
			if bedroom.Exists {
				continue
			}
			e, err := inspector.observe(s.Root, dir)
			if err != nil || e.Exists {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
			writes = append(writes, models.MigrationPlannedWrite{Path: dir, Kind: "directory", Role: "directory", Before: models.MigrationPathSnapshot{Path: dir}, ExpectedAfterState: "created"})
		}
		for _, m := range meta {
			if bedroom.Exists {
				continue
			}
			e, err := inspector.observe(s.Root, m.rel)
			if err != nil || e.Exists {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
			content, err := json.Marshal(m.value)
			if err != nil {
				return models.MaintenancePlanEnvelope{}, err
			}
			writes = append(writes, models.MigrationPlannedWrite{Path: m.rel, Kind: "json", Role: m.role, Before: models.MigrationPathSnapshot{Path: m.rel}, ExpectedAfterState: "created", Content: content})
		}
		parentBefore := models.MigrationPathSnapshot{Path: parent}
		if bedroom.Exists {
			e, err := inspector.observe(s.Root, parent)
			if err != nil || !e.Exists || e.Kind != "directory" {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrBedroomPresetUnavailable, err)
			}
			parentBefore.Exists, parentBefore.Kind, parentBefore.SHA256 = true, e.Kind, e.SHA256
		}
		items = append(items, models.MigrationPlanAction{ActionID: "roomlight_" + spec.ID, Action: "write_ir_command", SourceKind: bedroomPresetSourceKind, SourceFormat: "preset_command", TargetBefore: models.MigrationPathSnapshot{Path: target}, TargetParentBefore: parentBefore, PlannedWrites: writes, TargetFormat: models.OperationFormatPresetCommand, Transform: "preserve_preset_command", ExpectedAfterState: "target_created_no_source", Decision: &models.MigrationDecision{RoomID: "bedroom", DeviceID: "roomlight", TargetID: spec.ID, TargetName: spec.Name, PicoCommand: spec.Pico}})
	}
	// Fail closed if the backup anchor changed while the destination was read.
	current, err := inspector.observe(s.Root, "rooms")
	if err != nil || current != anchor {
		return models.MaintenancePlanEnvelope{}, errors.Join(ErrMaintenancePlanStale, err)
	}
	plan, err := s.Migration.Create(models.MigrationPlanPayload{Actions: items})
	if err != nil {
		return plan, err
	}
	if plan.PayloadSchemaID != "migration.v2" {
		return models.MaintenancePlanEnvelope{}, ErrMigrationPlanInvalid
	}
	return plan, nil
}

func (s *BedroomPresetPlanService) Load(id string) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.Migration == nil {
		return models.MaintenancePlanEnvelope{}, ErrBedroomPresetUnavailable
	}
	plan, err := s.Migration.Load(id)
	if err != nil {
		return plan, err
	}
	if plan.PayloadSchemaID != "migration.v2" {
		return models.MaintenancePlanEnvelope{}, ErrMigrationPlanInvalid
	}
	return plan, nil
}
