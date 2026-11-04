package services

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"backend/models"
	"backend/repositories"
)

type DeviceService struct {
	deviceRepository repositories.DeviceRepository
	roomRepository   repositories.RoomRepository
	trashDeletes     *CRUDTrashDeleteService
}

func NewDeviceService() DeviceService {
	return DeviceService{
		deviceRepository: repositories.NewDeviceRepository(),
		roomRepository:   repositories.NewRoomRepository(),
	}
}

func NewDeviceServiceWithRoomsDir(roomsDir string) DeviceService {
	return DeviceService{
		deviceRepository: repositories.NewDeviceRepositoryWithRoomsDir(roomsDir),
		roomRepository:   repositories.NewRoomRepositoryWithBaseDir(roomsDir),
	}
}

func (s DeviceService) WithTrashDeletes(deletes *CRUDTrashDeleteService) DeviceService {
	s.trashDeletes = deletes
	return s
}

var deviceIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

func (s DeviceService) ListDevices(roomID string) ([]models.Device, error) {
	if err := s.validateRoomExists(roomID); err != nil {
		return nil, err
	}

	devices, err := s.deviceRepository.List(roomID)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Device{}, nil
		}
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read devices directory",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].ID < devices[j].ID
	})

	return devices, nil
}

func (s DeviceService) GetDevice(roomID string, deviceID string) (models.Device, error) {
	if err := s.validateRoomAndDeviceIDs(roomID, deviceID); err != nil {
		return models.Device{}, err
	}

	if err := s.validateRoomExists(roomID); err != nil {
		return models.Device{}, err
	}

	device, err := s.deviceRepository.Load(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.Device{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "device not found",
				Err:     err,
			}
		}
		return models.Device{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load device",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return device, nil
}

func (s DeviceService) CreateDevice(roomID string, device models.Device) (models.Device, error) {
	roomID = strings.TrimSpace(roomID)
	device.ID = strings.TrimSpace(device.ID)
	device.Name = strings.TrimSpace(device.Name)
	device.RoomID = strings.TrimSpace(device.RoomID)
	device.DeviceType = strings.TrimSpace(device.DeviceType)
	device.PrimaryAdapter = strings.TrimSpace(device.PrimaryAdapter)

	if device.RoomID == "" {
		device.RoomID = roomID
	}

	if err := s.validateDeviceInput(roomID, device); err != nil {
		return models.Device{}, err
	}

	if err := s.validateRoomExists(roomID); err != nil {
		return models.Device{}, err
	}

	exists, err := s.deviceRepository.Exists(roomID, device.ID)
	if err != nil {
		return models.Device{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check device",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if exists {
		return models.Device{}, &ServiceError{
			Kind:    ErrorConflict,
			Message: "device already exists",
		}
	}

	if err := s.deviceRepository.Save(device); err != nil {
		return models.Device{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save device",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if device.PrimaryAdapter == "ir" {
		if err := s.deviceRepository.EnsureAdapterDir(roomID, device.ID, "ir"); err != nil {
			return models.Device{}, &ServiceError{
				Kind:    ErrorInternal,
				Message: "failed to create device adapter directory",
				Detail:  err.Error(),
				Err:     err,
			}
		}
	}

	return device, nil
}

func (s DeviceService) UpdateDevice(roomID string, deviceID string, device models.Device) (models.Device, error) {
	roomID = strings.TrimSpace(roomID)
	deviceID = strings.TrimSpace(deviceID)
	device.ID = strings.TrimSpace(device.ID)
	device.Name = strings.TrimSpace(device.Name)
	device.RoomID = strings.TrimSpace(device.RoomID)
	device.DeviceType = strings.TrimSpace(device.DeviceType)
	device.PrimaryAdapter = strings.TrimSpace(device.PrimaryAdapter)

	if device.ID == "" {
		device.ID = deviceID
	}
	if device.RoomID == "" {
		device.RoomID = roomID
	}

	if device.ID != deviceID {
		return models.Device{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "device id mismatch",
		}
	}

	if err := s.validateDeviceInput(roomID, device); err != nil {
		return models.Device{}, err
	}

	if err := s.validateRoomExists(roomID); err != nil {
		return models.Device{}, err
	}

	_, err := s.deviceRepository.Load(roomID, deviceID)
	if err != nil {
		if os.IsNotExist(err) {
			return models.Device{}, &ServiceError{Kind: ErrorNotFound, Message: "device not found", Err: err}
		}
		return models.Device{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load device",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if err := s.deviceRepository.Save(device); err != nil {
		return models.Device{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save device",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if device.PrimaryAdapter == "ir" {
		if err := s.deviceRepository.EnsureAdapterDir(roomID, device.ID, "ir"); err != nil {
			return models.Device{}, &ServiceError{
				Kind:    ErrorInternal,
				Message: "failed to create device adapter directory",
				Detail:  err.Error(),
				Err:     err,
			}
		}
	}

	return device, nil
}

func (s DeviceService) DeleteDevice(roomID string, deviceID string) (CRUDDeleteResult, error) {
	if err := s.validateRoomAndDeviceIDs(roomID, deviceID); err != nil {
		return CRUDDeleteResult{}, err
	}

	if err := s.validateRoomExists(roomID); err != nil {
		return CRUDDeleteResult{}, err
	}

	if _, err := s.deviceRepository.Load(roomID, deviceID); err != nil {
		if os.IsNotExist(err) {
			return CRUDDeleteResult{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "device not found",
				Err:     err,
			}
		}
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read device before delete",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if s.trashDeletes == nil {
		return CRUDDeleteResult{}, ErrMaintenancePlanNotReady
	}
	relativePath := filepath.ToSlash(filepath.Join("rooms", roomID, "devices", deviceID, "device.json"))
	return s.trashDeletes.DeleteFile("device", deviceID, relativePath)
}

func (s DeviceService) validateDeviceInput(roomID string, device models.Device) error {
	if err := s.validateRoomAndDeviceIDs(roomID, device.ID); err != nil {
		return err
	}

	if device.RoomID != roomID {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "room id mismatch",
		}
	}

	if device.Name == "" {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "device name is required",
		}
	}

	if !validateDeviceType(device.DeviceType) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid device type",
		}
	}

	if !validatePrimaryAdapter(device.PrimaryAdapter) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid primary adapter",
		}
	}

	return nil
}

func (s DeviceService) validateRoomAndDeviceIDs(roomID string, deviceID string) error {
	if !validateRoomID(roomID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	if !validateDeviceID(deviceID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid device id",
		}
	}

	return nil
}

func (s DeviceService) validateRoomExists(roomID string) error {
	if !validateRoomID(roomID) {
		return &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	exists, err := s.roomRepository.Exists(roomID)
	if err != nil {
		return &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check room",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if !exists {
		return &ServiceError{
			Kind:    ErrorNotFound,
			Message: "room not found",
		}
	}

	return nil
}

func validateDeviceID(id string) bool {
	return deviceIDPattern.MatchString(id)
}

func validateDeviceType(deviceType string) bool {
	switch deviceType {
	case "light", "air_conditioner", "tv", "fan", "switch", "pc", "unknown":
		return true
	default:
		return false
	}
}

func validatePrimaryAdapter(adapter string) bool {
	switch adapter {
	case "ir", "echonet_lite", "matter":
		return true
	default:
		return false
	}
}
