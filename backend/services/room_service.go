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

type RoomService struct {
	repository   repositories.RoomRepository
	trashDeletes *CRUDTrashDeleteService
}

func NewRoomService() RoomService {
	return RoomService{repository: repositories.NewRoomRepository()}
}

func NewRoomServiceWithBaseDir(baseDir string) RoomService {
	return RoomService{repository: repositories.NewRoomRepositoryWithBaseDir(baseDir)}
}

func (s RoomService) WithTrashDeletes(deletes *CRUDTrashDeleteService) RoomService {
	s.trashDeletes = deletes
	return s
}

var roomIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

func (s RoomService) ListRooms() ([]models.Room, error) {
	rooms, err := s.repository.List()
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Room{}, nil
		}
		return nil, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to read rooms directory",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].SortOrder == rooms[j].SortOrder {
			return rooms[i].ID < rooms[j].ID
		}
		return rooms[i].SortOrder < rooms[j].SortOrder
	})

	return rooms, nil
}

func (s RoomService) GetRoom(id string) (models.Room, error) {
	if !validateRoomID(id) {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	room, err := s.repository.Load(id)
	if err != nil {
		if os.IsNotExist(err) {
			return models.Room{}, &ServiceError{
				Kind:    ErrorNotFound,
				Message: "room not found",
				Err:     err,
			}
		}
		return models.Room{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load room",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return room, nil
}

func (s RoomService) CreateRoom(room models.Room) (models.Room, error) {
	room.ID = strings.TrimSpace(room.ID)
	room.Name = strings.TrimSpace(room.Name)

	if !validateRoomID(room.ID) {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	if room.Name == "" {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "room name is required",
		}
	}

	exists, err := s.repository.Exists(room.ID)
	if err != nil {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check room",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if exists {
		return models.Room{}, &ServiceError{
			Kind:    ErrorConflict,
			Message: "room already exists",
		}
	}

	if err := s.repository.Save(room); err != nil {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save room",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return room, nil
}

func (s RoomService) UpdateRoom(id string, room models.Room) (models.Room, error) {
	id = strings.TrimSpace(id)
	room.ID = strings.TrimSpace(room.ID)
	room.Name = strings.TrimSpace(room.Name)

	if !validateRoomID(id) {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	if room.ID == "" {
		room.ID = id
	}
	if room.ID != id {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "room id mismatch",
		}
	}

	if room.Name == "" {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "room name is required",
		}
	}

	_, err := s.repository.Load(id)
	if err != nil {
		if os.IsNotExist(err) {
			return models.Room{}, &ServiceError{Kind: ErrorNotFound, Message: "room not found", Err: err}
		}
		return models.Room{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to load room",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	if err := s.repository.Save(room); err != nil {
		return models.Room{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to save room",
			Detail:  err.Error(),
			Err:     err,
		}
	}

	return room, nil
}

func (s RoomService) DeleteRoom(id string) (CRUDDeleteResult, error) {
	if !validateRoomID(id) {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInvalidRequest,
			Message: "invalid room id",
		}
	}

	exists, err := s.repository.Exists(id)
	if err != nil {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check room",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if !exists {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorNotFound,
			Message: "room not found",
		}
	}

	hasDevices, err := s.repository.HasDevices(id)
	if err != nil {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorInternal,
			Message: "failed to check room devices",
			Detail:  err.Error(),
			Err:     err,
		}
	}
	if hasDevices {
		return CRUDDeleteResult{}, &ServiceError{
			Kind:    ErrorConflict,
			Message: "room has devices",
		}
	}

	if s.trashDeletes == nil {
		return CRUDDeleteResult{}, ErrMaintenancePlanNotReady
	}
	return s.trashDeletes.DeleteFile("room", id, filepath.ToSlash(filepath.Join("rooms", id, "room.json")))
}

func validateRoomID(id string) bool {
	return roomIDPattern.MatchString(id)
}
