package repositories

import (
	"encoding/json"
	"os"
	"path/filepath"

	"backend/models"
)

type DeviceRepository struct {
	roomsDir string
	writer   JSONWriter
}

func NewDeviceRepository() DeviceRepository {
	return NewDeviceRepositoryWithRoomsDir(defaultRoomsDir)
}

func NewDeviceRepositoryWithRoomsDir(roomsDir string) DeviceRepository {
	return DeviceRepository{roomsDir: roomsDir, writer: NewAtomicJSONWriter()}
}

func (r DeviceRepository) List(roomID string) ([]models.Device, error) {
	entries, err := os.ReadDir(r.devicesDir(roomID))
	if err != nil {
		return nil, err
	}

	devices := []models.Device{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		device, err := r.Load(roomID, entry.Name())
		if err != nil {
			continue
		}

		devices = append(devices, device)
	}

	return devices, nil
}

func (r DeviceRepository) Load(roomID string, deviceID string) (models.Device, error) {
	data, err := os.ReadFile(r.devicePath(roomID, deviceID))
	if err != nil {
		return models.Device{}, err
	}

	var device models.Device
	if err := json.Unmarshal(data, &device); err != nil {
		return models.Device{}, err
	}

	return device, nil
}

func (r DeviceRepository) Save(device models.Device) error {
	if err := os.MkdirAll(r.deviceDir(device.RoomID, device.ID), 0755); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.devicePath(device.RoomID, device.ID), device, 0644)
}

func (r DeviceRepository) Delete(roomID string, deviceID string) error {
	return os.Remove(r.devicePath(roomID, deviceID))
}

func (r DeviceRepository) Exists(roomID string, deviceID string) (bool, error) {
	_, err := os.Stat(r.devicePath(roomID, deviceID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r DeviceRepository) EnsureAdapterDir(roomID string, deviceID string, adapterType string) error {
	return os.MkdirAll(filepath.Join(r.deviceDir(roomID, deviceID), "adapters", adapterType), 0755)
}

func (r DeviceRepository) devicesDir(roomID string) string {
	return filepath.Join(r.roomsDir, roomID, "devices")
}

func (r DeviceRepository) deviceDir(roomID string, deviceID string) string {
	return filepath.Join(r.devicesDir(roomID), deviceID)
}

func (r DeviceRepository) devicePath(roomID string, deviceID string) string {
	return filepath.Join(r.deviceDir(roomID, deviceID), "device.json")
}
