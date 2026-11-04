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
	"strings"

	"backend/ir"
	"backend/models"
	"backend/repositories"
)

var ErrMigrationDryRunInvalid = errors.New("invalid migration mapping or source")
var ErrMigrationDryRunConflict = errors.New("migration target exists or source changed")

type MigrationDryRunService struct {
	Root  string
	Plans *MigrationPlanService
}

func NewMigrationDryRunService(root string, plans *MigrationPlanService) *MigrationDryRunService {
	return &MigrationDryRunService{Root: root, Plans: plans}
}

func (s *MigrationDryRunService) Create(request models.MigrationDryRunRequest) (models.MaintenancePlanEnvelope, error) {
	if s == nil || s.Plans == nil || !filepath.IsAbs(s.Root) || request.Validate() != nil {
		return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
	}
	inspector := MaintenanceFilesystemInspector{}
	items := make([]models.MigrationPlanAction, 0, len(request.Mappings))
	for _, m := range request.Mappings {
		if !planActionIDPattern.MatchString(m.ActionID) || !planActionIDPattern.MatchString(m.RoomID) || !planActionIDPattern.MatchString(m.DeviceID) || !planActionIDPattern.MatchString(m.TargetID) || !validSourcePath(m.SourcePath) || path.Dir(m.SourcePath) != "ir-signals" || !strings.HasSuffix(m.SourcePath, ".json") {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		sourceEntry, err := inspector.observe(s.Root, m.SourcePath)
		if err != nil || !sourceEntry.Exists || sourceEntry.Kind != "file" {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		filePath := filepath.Join(s.Root, filepath.FromSlash(m.SourcePath))
		data, err := os.ReadFile(filePath)
		if err != nil || len(data) == 0 || len(data) > 4*1024*1024 {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		info, err := os.Lstat(filePath)
		if err != nil || !info.Mode().IsRegular() {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		digest, err := hashObservedFile(filePath, info)
		contentHash := sha256.Sum256(data)
		if err != nil || digest != sourceEntry.SHA256 || digest != hex.EncodeToString(contentHash[:]) {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunConflict, err)
		}
		var signal models.MigrationSourceSignal
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&signal) != nil || decoder.Decode(new(any)) != io.EOF || signal.Name != strings.TrimSuffix(path.Base(m.SourcePath), ".json") || !validMigrationSignal(signal) {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		kindDir, action := "commands", "write_ir_command"
		if m.TargetKind == "state" {
			kindDir, action = "states", "write_ir_state"
		}
		target := path.Join("rooms", m.RoomID, "devices", m.DeviceID, "adapters", "ir", kindDir, m.TargetID+".json")
		parent := path.Dir(target)
		targetEntry, err := inspector.observe(s.Root, target)
		if err != nil {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		if targetEntry.Exists {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunConflict
		}
		parentEntry, err := inspector.observe(s.Root, parent)
		if err != nil {
			return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		if parentEntry.Exists && parentEntry.Kind != "directory" {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunConflict
		}
		before := models.MigrationPathSnapshot{Path: target}
		writes := []models.MigrationPlannedWrite{{Path: target, Kind: "json", Role: "operation", Before: before, ExpectedAfterState: "created"}}
		for dir := parent; dir != "."; dir = path.Dir(dir) {
			e, err := inspector.observe(s.Root, dir)
			if err != nil || e.Exists && e.Kind != "directory" {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunInvalid, err)
			}
			if !e.Exists {
				writes = append(writes, models.MigrationPlannedWrite{Path: dir, Kind: "directory", Role: "directory", Before: models.MigrationPathSnapshot{Path: dir}, ExpectedAfterState: "created"})
			}
		}
		roomsDir := filepath.Join(s.Root, "rooms")
		roomPath := path.Join("rooms", m.RoomID, "room.json")
		room, err := migrationMetadata(s.Root, inspector, roomPath, m.Room, func() (any, error) { return repositories.NewRoomRepositoryWithBaseDir(roomsDir).Load(m.RoomID) })
		if err != nil {
			return models.MaintenancePlanEnvelope{}, err
		}
		roomValue, ok := room.(models.Room)
		if !ok || roomValue.ID != m.RoomID || strings.TrimSpace(roomValue.Name) == "" {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		if w := migrationMetadataWrite(roomPath, m.Room, inspector, s.Root); w != nil {
			writes = append(writes, *w)
		}
		devicePath := path.Join("rooms", m.RoomID, "devices", m.DeviceID, "device.json")
		device, err := migrationMetadata(s.Root, inspector, devicePath, m.Device, func() (any, error) {
			return repositories.NewDeviceRepositoryWithRoomsDir(roomsDir).Load(m.RoomID, m.DeviceID)
		})
		if err != nil {
			return models.MaintenancePlanEnvelope{}, err
		}
		deviceValue, ok := device.(models.Device)
		if !ok || deviceValue.ID != m.DeviceID || deviceValue.RoomID != m.RoomID || strings.TrimSpace(deviceValue.Name) == "" || deviceValue.DeviceType == "" || deviceValue.PrimaryAdapter != "ir" {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		if w := migrationMetadataWrite(devicePath, m.Device, inspector, s.Root); w != nil {
			writes = append(writes, *w)
		}
		configPath := path.Join("rooms", m.RoomID, "devices", m.DeviceID, "adapters", "ir", "ir_config.json")
		adapterRepo := repositories.NewIRAdapterRepositoryWithRoomsDir(roomsDir)
		config, err := migrationMetadata(s.Root, inspector, configPath, m.IRConfig, func() (any, error) { return adapterRepo.LoadConfig(m.RoomID, m.DeviceID) })
		if err != nil {
			return models.MaintenancePlanEnvelope{}, err
		}
		configValue, ok := config.(models.IRConfig)
		expectedType := models.IRRemoteTypeCommand
		if m.TargetKind == "state" {
			expectedType = models.IRRemoteTypeStateful
		}
		if !ok || configValue.IRRemoteType != expectedType || configValue.CarrierKHz <= 0 || configValue.CarrierKHz != signal.CarrierKHz {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		if w := migrationMetadataWrite(configPath, m.IRConfig, inspector, s.Root); w != nil {
			writes = append(writes, *w)
		}
		if m.TargetKind == "state" {
			schemaPath := path.Join("rooms", m.RoomID, "devices", m.DeviceID, "adapters", "ir", "state_schema.json")
			var supplied *models.IRStateSchema
			if m.StateSchema != nil {
				supplied = &m.StateSchema
			}
			schema, err := migrationMetadata(s.Root, inspector, schemaPath, supplied, func() (any, error) { return adapterRepo.LoadStateSchema(m.RoomID, m.DeviceID) })
			if err != nil {
				return models.MaintenancePlanEnvelope{}, err
			}
			schemaValue, ok := schema.(models.IRStateSchema)
			if !ok || validateIRStateSchema(schemaValue) != nil {
				return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
			}
			if m.Decision != nil && len(m.Decision.State) > 0 && validateIRStateMatchesSchema(m.Decision.State, schemaValue) != nil {
				return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
			}
			if w := migrationMetadataWrite(schemaPath, supplied, inspector, s.Root); w != nil {
				writes = append(writes, *w)
			}
		} else if m.StateSchema != nil {
			return models.MaintenancePlanEnvelope{}, ErrMigrationDryRunInvalid
		}
		targetFormat, transform := migrationFormat(signal.Format)
		item := models.MigrationPlanAction{ActionID: m.ActionID, Action: action, Source: models.MigrationPathSnapshot{Path: m.SourcePath, Exists: true, Kind: "file", Size: int64(len(data)), SHA256: digest}, SourceFormat: signal.Format, TargetBefore: before, TargetParentBefore: models.MigrationPathSnapshot{Path: parent, Exists: parentEntry.Exists, Kind: parentEntry.Kind, SHA256: parentEntry.SHA256}, PlannedWrites: writes, TargetFormat: targetFormat, Transform: transform, ExpectedAfterState: "target_created_source_preserved", Decision: m.Decision}
		items = append(items, item)
	}
	for _, item := range items {
		for _, write := range item.PlannedWrites {
			e, err := inspector.observe(s.Root, write.Path)
			if err != nil || e.Exists {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunConflict, err)
			}
		}
		if item.TargetParentBefore.Exists {
			e, err := inspector.observe(s.Root, item.TargetParentBefore.Path)
			if err != nil || !e.Exists || e.Kind != "directory" || e.SHA256 != item.TargetParentBefore.SHA256 {
				return models.MaintenancePlanEnvelope{}, errors.Join(ErrMigrationDryRunConflict, err)
			}
		}
	}
	return s.Plans.Create(models.MigrationPlanPayload{Actions: items})
}

func migrationMetadata(root string, inspector MaintenanceFilesystemInspector, rel string, supplied any, load func() (any, error)) (any, error) {
	if supplied != nil && reflect.ValueOf(supplied).Kind() == reflect.Pointer && reflect.ValueOf(supplied).IsNil() {
		supplied = nil
	}
	e, err := inspector.observe(root, rel)
	if err != nil {
		return nil, errors.Join(ErrMigrationDryRunInvalid, err)
	}
	if e.Exists {
		if e.Kind != "file" || supplied != nil {
			return nil, ErrMigrationDryRunConflict
		}
		value, err := load()
		if err != nil {
			return nil, errors.Join(ErrMigrationDryRunInvalid, err)
		}
		// Repository readback is required, then strict decode and a second hash
		// bind the observed bytes to the typed value used by this plan.
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || len(data) == 0 || len(data) > 4096 {
			return nil, ErrMigrationDryRunInvalid
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, ErrMigrationDryRunConflict
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var typed any
		switch value.(type) {
		case models.Room:
			typed = new(models.Room)
		case models.Device:
			typed = new(models.Device)
		case models.IRConfig:
			typed = new(models.IRConfig)
		case models.IRStateSchema:
			typed = new(models.IRStateSchema)
		default:
			return nil, ErrMigrationDryRunInvalid
		}
		if decoder.Decode(typed) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, ErrMigrationDryRunInvalid
		}
		return value, nil
	}
	if supplied == nil {
		return nil, ErrMigrationDryRunInvalid
	}
	data, err := json.Marshal(supplied)
	if err != nil || len(data) > 4096 {
		return nil, ErrMigrationDryRunInvalid
	}
	switch value := supplied.(type) {
	case *models.Room:
		return *value, nil
	case *models.Device:
		return *value, nil
	case *models.IRConfig:
		return *value, nil
	case *models.IRStateSchema:
		return *value, nil
	default:
		return nil, ErrMigrationDryRunInvalid
	}
}

func migrationMetadataWrite(rel string, supplied any, inspector MaintenanceFilesystemInspector, root string) *models.MigrationPlannedWrite {
	if supplied == nil || reflect.ValueOf(supplied).Kind() == reflect.Pointer && reflect.ValueOf(supplied).IsNil() {
		return nil
	}
	e, err := inspector.observe(root, rel)
	if err != nil || e.Exists {
		return nil
	}
	data, err := json.Marshal(supplied)
	if err != nil {
		return nil
	}
	role := path.Base(rel)
	switch role {
	case "room.json":
		role = "room"
	case "device.json":
		role = "device"
	case "ir_config.json":
		role = "ir_config"
	case "state_schema.json":
		role = "state_schema"
	}
	return &models.MigrationPlannedWrite{Path: rel, Kind: "json", Role: role, Before: models.MigrationPathSnapshot{Path: rel}, ExpectedAfterState: "created", Content: data}
}

func migrationFormat(format string) (models.OperationFormat, string) {
	switch format {
	case "raw_samples":
		return models.OperationFormatRawSamples, "preserve_raw_samples"
	case "raw":
		return models.OperationFormatRaw, "preserve_raw"
	case "raw_median":
		return models.OperationFormatRaw, "raw_median_to_raw"
	case "preset_command":
		return models.OperationFormatPresetCommand, "preserve_preset_command"
	default:
		return "", ""
	}
}

func validMigrationSignal(signal models.MigrationSourceSignal) bool {
	if signal.Name == "" || signal.CarrierKHz <= 0 {
		return false
	}
	switch signal.Format {
	case "raw", "raw_median":
		return ir.ValidateRawTimings(signal.TimingsUS, signal.Count) == nil
	case "raw_samples":
		if len(signal.Captures) == 0 {
			return false
		}
		for _, capture := range signal.Captures {
			if capture.Valid && !capture.OK || capture.Count != len(capture.TimingsUS) || len(capture.TimingsUS) > 0 && ir.ValidateRawTimings(capture.TimingsUS, capture.Count) != nil {
				return false
			}
		}
		representative, err := ir.InferRepresentativeTimings(signal.Captures)
		return err == nil && ir.ValidateRawTimings(representative, len(representative)) == nil && ir.ValidateRawTimings(signal.TimingsUS, signal.Count) == nil && reflect.DeepEqual(representative, signal.TimingsUS)
	case "preset_command":
		return true
	default:
		return false
	}
}
