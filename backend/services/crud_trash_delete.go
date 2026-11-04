package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"backend/models"
	"backend/repositories"
)

type crudDeleteOperationStore interface {
	MaintenanceOperationStore
	Load(string) (models.MaintenanceOperation, error)
}

type crudDeleteTrashStore interface {
	Move(repositories.TrashMoveRequest) (repositories.TrashMoveResult, error)
	Load(string) (models.TrashEntry, error)
}

// CRUDTrashDeleteService moves one already-validated metadata JSON file to
// Trash and records the move as a verified, planless delete operation.
type CRUDTrashDeleteService struct {
	operations crudDeleteOperationStore
	trash      crudDeleteTrashStore
	inspector  MaintenanceFilesystemInspector
	newID      func(string) (string, error)
}

type CRUDDeleteResult struct {
	OperationID  string `json:"operation_id"`
	TrashEntryID string `json:"trash_entry_id"`
	Status       string `json:"status"`
}

func NewCRUDTrashDeleteService(operations crudDeleteOperationStore, trash crudDeleteTrashStore, runtimeRoot, trashRoot string) *CRUDTrashDeleteService {
	return &CRUDTrashDeleteService{
		operations: operations,
		trash:      trash,
		inspector:  MaintenanceFilesystemInspector{RuntimeRoot: runtimeRoot, TrashRoot: trashRoot},
		newID:      newCRUDDeleteID,
	}
}

func newCRUDDeleteID(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random[:]), nil
}

// DeleteFile accepts only the metadata paths owned by the ordinary Room,
// Device, IR Command and IR State DELETE routes. It never recursively moves a
// Room/Device directory or removes associated data outside the selected file.
func (s *CRUDTrashDeleteService) DeleteFile(targetType, originalID, sourceRelativePath string) (CRUDDeleteResult, error) {
	if s == nil || s.operations == nil || s.trash == nil || s.newID == nil || s.inspector.RuntimeRoot == "" || s.inspector.TrashRoot == "" {
		return CRUDDeleteResult{}, ErrMaintenancePlanNotReady
	}
	if !validCRUDDeleteTarget(targetType, originalID, sourceRelativePath) {
		return CRUDDeleteResult{}, errors.New("invalid CRUD delete target")
	}
	opID, err := s.newID("delete_")
	if err != nil {
		return CRUDDeleteResult{}, err
	}
	entryID, err := s.newID("trash_")
	if err != nil {
		return CRUDDeleteResult{}, err
	}
	beforeRuntime, err := s.inspector.observe(s.inspector.RuntimeRoot, sourceRelativePath)
	if err != nil || !beforeRuntime.Exists || beforeRuntime.Kind != "file" {
		return CRUDDeleteResult{}, errors.Join(ErrCRUDDeleteTargetStale, err)
	}
	trashRelative := entryID + "/payload"
	beforeTrash, err := s.inspector.observe(s.inspector.TrashRoot, trashRelative)
	if err != nil || beforeTrash.Exists {
		return CRUDDeleteResult{}, errors.Join(ErrCRUDDeleteTargetStale, err)
	}
	afterRuntime := beforeRuntime
	afterRuntime.Exists, afterRuntime.Kind, afterRuntime.SHA256 = false, "", ""
	afterTrash := beforeRuntime
	afterTrash.Path = trashRelative
	before := models.MaintenanceSnapshot{Runtime: &beforeRuntime, Trash: &beforeTrash}
	intended := models.MaintenanceSnapshot{Runtime: &afterRuntime, Trash: &afterTrash}
	beforeJSON, _ := json.Marshal(before)
	intendedJSON, _ := json.Marshal(intended)
	work := []MaintenanceActionWork{{ID: "delete_target", Action: "trash_file", BeforeState: beforeJSON, IntendedState: intendedJSON,
		Mutate: func() error {
			current, err := s.inspector.observe(s.inspector.RuntimeRoot, sourceRelativePath)
			if err != nil || current != beforeRuntime {
				return errors.Join(ErrCRUDDeleteTargetStale, err)
			}
			currentTrash, err := s.inspector.observe(s.inspector.TrashRoot, trashRelative)
			if err != nil || currentTrash.Exists {
				return errors.Join(ErrCRUDDeleteTargetStale, err)
			}
			_, err = s.trash.Move(repositories.TrashMoveRequest{
				EntryID: entryID, SourceRelativePath: sourceRelativePath, OriginalID: originalID,
				TargetType: targetType, Reason: "user_delete", OperationType: "delete", OperationID: opID,
			})
			return err
		},
		Readback: func() (json.RawMessage, error) {
			entry, err := s.trash.Load(entryID)
			if err != nil {
				return nil, err
			}
			if entry.Status != "sealed" || !entry.RestoreAvailable || entry.OperationType != "delete" || entry.OperationID != opID || entry.TargetType != targetType || entry.OriginalID != originalID || entry.OriginalRelativePath != sourceRelativePath || entry.BeforeInventory.Kind != "file" {
				return nil, fmt.Errorf("Trash delete readback mismatch")
			}
			runtime, err := s.inspector.observe(s.inspector.RuntimeRoot, sourceRelativePath)
			if err != nil {
				return nil, err
			}
			payload, err := s.inspector.observe(s.inspector.TrashRoot, trashRelative)
			if err != nil {
				return nil, err
			}
			actual := models.MaintenanceSnapshot{Runtime: &runtime, Trash: &payload}
			if !sameSnapshot(actual, intended) {
				return nil, errors.New("CRUD delete filesystem readback mismatch")
			}
			return json.Marshal(actual)
		},
	}}
	draft := models.MaintenanceOperation{ID: opID, Target: sourceRelativePath, Type: "delete", ClaimedPaths: []string{sourceRelativePath}}
	operation, err := NewMaintenanceCoordinator(s.operations).Run(draft, work)
	if err != nil {
		if stored, loadErr := s.operations.Load(opID); loadErr == nil {
			return CRUDDeleteResult{OperationID: stored.ID, TrashEntryID: entryID, Status: stored.Status}, err
		}
		if errors.Is(err, repositories.ErrMaintenanceTargetClaimed) || errors.Is(err, repositories.ErrMaintenanceDuplicate) {
			return CRUDDeleteResult{}, err
		}
		// An operation-create/readback error can leave durability uncertain. Give
		// operators both generated identifiers and prohibit automatic retry.
		return CRUDDeleteResult{OperationID: opID, TrashEntryID: entryID, Status: "unknown"}, err
	}
	return CRUDDeleteResult{OperationID: operation.ID, TrashEntryID: entryID, Status: operation.Status}, nil
}

var ErrCRUDDeleteTargetStale = errors.New("CRUD delete target changed")

func validCRUDDeleteTarget(targetType, originalID, rel string) bool {
	if originalID == "" || filepath.IsAbs(rel) || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.Contains(rel, "\\") {
		return false
	}
	parts := strings.Split(rel, "/")
	switch targetType {
	case "room":
		return len(parts) == 3 && parts[0] == "rooms" && parts[1] == originalID && validateRoomID(parts[1]) && parts[2] == "room.json"
	case "device":
		return len(parts) == 5 && parts[0] == "rooms" && validateRoomID(parts[1]) && parts[2] == "devices" && parts[3] == originalID && validateDeviceID(parts[3]) && parts[4] == "device.json"
	case "ir_command":
		return len(parts) == 8 && parts[0] == "rooms" && validateRoomID(parts[1]) && parts[2] == "devices" && validateDeviceID(parts[3]) && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "commands" && parts[7] == originalID+".json" && validateIRCommandID(originalID)
	case "ir_state":
		return len(parts) == 8 && parts[0] == "rooms" && validateRoomID(parts[1]) && parts[2] == "devices" && validateDeviceID(parts[3]) && parts[4] == "adapters" && parts[5] == "ir" && parts[6] == "states" && parts[7] == originalID+".json" && validateIRStateID(originalID)
	default:
		return false
	}
}
