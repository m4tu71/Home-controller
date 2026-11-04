package repositories

import (
	"encoding/json"
	"os"
	"path/filepath"

	"backend/models"
)

const defaultRoomsDir = "/var/lib/web-controller/data/rooms"

type RoomRepository struct {
	baseDir string
	writer  JSONWriter
}

func NewRoomRepository() RoomRepository {
	return NewRoomRepositoryWithBaseDir(defaultRoomsDir)
}

func NewRoomRepositoryWithBaseDir(baseDir string) RoomRepository {
	return RoomRepository{baseDir: baseDir, writer: NewAtomicJSONWriter()}
}

func (r RoomRepository) List() ([]models.Room, error) {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		return nil, err
	}

	rooms := []models.Room{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		room, err := r.Load(entry.Name())
		if err != nil {
			continue
		}

		rooms = append(rooms, room)
	}

	return rooms, nil
}

func (r RoomRepository) Load(id string) (models.Room, error) {
	data, err := os.ReadFile(r.roomPath(id))
	if err != nil {
		return models.Room{}, err
	}

	var room models.Room
	if err := json.Unmarshal(data, &room); err != nil {
		return models.Room{}, err
	}

	return room, nil
}

func (r RoomRepository) Save(room models.Room) error {
	if err := os.MkdirAll(r.roomDir(room.ID), 0755); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.roomPath(room.ID), room, 0644)
}

func (r RoomRepository) Delete(id string) error {
	return os.Remove(r.roomPath(id))
}

func (r RoomRepository) HasDevices(id string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(r.roomDir(id), "devices"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if _, err := os.Stat(filepath.Join(r.roomDir(id), "devices", entry.Name(), "device.json")); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}

	return false, nil
}

func (r RoomRepository) Exists(id string) (bool, error) {
	_, err := os.Stat(r.roomPath(id))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r RoomRepository) roomDir(id string) string {
	return filepath.Join(r.baseDir, id)
}

func (r RoomRepository) roomPath(id string) string {
	return filepath.Join(r.roomDir(id), "room.json")
}
