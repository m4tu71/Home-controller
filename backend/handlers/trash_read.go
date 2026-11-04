package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

const trashReadPath = "/api/controller/maintenance/trash"

type trashReader interface {
	List(services.TrashListQuery) (services.TrashListPage, error)
	Detail(string) (models.TrashEntry, error)
}

type trashRestorer interface {
	DryRun(string) (repositories.TrashRestorePreview, error)
	Restore(string, string) (models.TrashEntry, string, error)
}
type TrashReadHandler struct {
	reader   trashReader
	restorer trashRestorer
}

func NewTrashReadHandler(reader *services.TrashReadService) *TrashReadHandler {
	return &TrashReadHandler{reader: reader}
}

func (h *TrashReadHandler) WithRestore(restorer *services.TrashRestoreService) *TrashReadHandler {
	h.restorer = restorer
	return h
}

type trashEntryView struct {
	EntryID                 string     `json:"entry_id"`
	Status                  string     `json:"status"`
	TargetType              string     `json:"target_type"`
	OriginalID              string     `json:"original_id"`
	OriginalRelativePath    string     `json:"original_relative_path"`
	DeletedAt               *time.Time `json:"deleted_at"`
	EligibleAfter           *time.Time `json:"eligible_after"`
	LastCheckedAt           *time.Time `json:"last_checked_at,omitempty"`
	FinalDeletedAt          *time.Time `json:"final_deleted_at,omitempty"`
	FinalDeleteFilesDeleted int64      `json:"final_delete_files_deleted,omitempty"`
	FinalDeleteFilesTotal   int64      `json:"final_delete_files_total,omitempty"`
	Reason                  string     `json:"reason"`
	OperationType           string     `json:"operation_type"`
	OperationID             string     `json:"operation_id"`
	RestoreAvailable        bool       `json:"restore_available"`
}

type trashDetailView struct {
	trashEntryView
	Inventory models.TrashInventory `json:"inventory"`
}

func (h *TrashReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == trashReadPath+"/final-delete" || strings.HasPrefix(r.URL.Path, trashReadPath+"/final-delete/") {
		writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
		return
	}
	if isTrashRestoreRoute(r.URL.Path) && r.Method != http.MethodPost {
		writeMaintenanceMethodNotAllowed(w, http.MethodPost)
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, trashReadPath+"/") {
		h.serveRestore(w, r)
		return
	}
	root := r.URL.Path == trashReadPath
	id := ""
	if !root {
		if !strings.HasPrefix(r.URL.Path, trashReadPath+"/") {
			writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
			return
		}
		id = strings.TrimPrefix(r.URL.Path, trashReadPath+"/")
		if id == "" || strings.Contains(id, "/") {
			writeMaintenanceInputError(w, http.StatusNotFound, "route not found")
			return
		}
	}
	if r.Method != http.MethodGet {
		writeMaintenanceMethodNotAllowed(w, http.MethodGet)
		return
	}
	if h == nil || h.reader == nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "Trash history unavailable")
		return
	}
	if !root && safeMaintenanceToken(id) != id {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid Trash entry ID")
		return
	}
	if root {
		query, err := parseTrashListQuery(r.URL.RawQuery)
		if err != nil {
			writeMaintenanceInputError(w, http.StatusBadRequest, "invalid Trash list query")
			return
		}
		page, err := h.reader.List(query)
		if err != nil {
			writeMaintenanceInputError(w, http.StatusServiceUnavailable, "Trash history unavailable")
			return
		}
		entries := make([]trashEntryView, 0, len(page.Entries))
		for _, entry := range page.Entries {
			entries = append(entries, trashViewOf(entry))
		}
		writeMaintenanceJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": page.Total, "offset": page.Offset, "limit": page.Limit})
		return
	}
	entry, err := h.reader.Detail(id)
	if errors.Is(err, repositories.ErrMaintenanceID) {
		writeMaintenanceInputError(w, http.StatusBadRequest, "invalid Trash entry ID")
		return
	}
	if errors.Is(err, repositories.ErrTrashMissing) {
		writeMaintenanceInputError(w, http.StatusNotFound, "Trash entry not found")
		return
	}
	if err != nil {
		writeMaintenanceInputError(w, http.StatusServiceUnavailable, "Trash history unavailable")
		return
	}
	writeMaintenanceJSON(w, http.StatusOK, trashDetailView{trashEntryView: trashViewOf(entry), Inventory: entry.BeforeInventory})
}

func isTrashRestoreRoute(path string) bool {
	if !strings.HasPrefix(path, trashReadPath+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, trashReadPath+"/"), "/")
	return parts[0] != "" && ((len(parts) == 2 && parts[1] == "restore") || (len(parts) == 3 && parts[1] == "restore" && parts[2] == "dry-run"))
}

func (h *TrashReadHandler) serveRestore(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, trashReadPath+"/")
	parts := strings.Split(path, "/")
	if (len(parts) != 3 || parts[2] != "dry-run") && (len(parts) != 2) {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	if parts[1] != "restore" {
		writeMaintenanceInputError(w, 404, "route not found")
		return
	}
	id := parts[0]
	if id == "" || safeMaintenanceToken(id) != id {
		writeMaintenanceInputError(w, 400, "invalid Trash entry ID")
		return
	}
	if h == nil || h.restorer == nil {
		writeMaintenanceInputError(w, 503, "Trash restore unavailable")
		return
	}
	if len(parts) == 3 {
		if !RequireEmptyMaintenanceBody(w, r, MaintenanceJSONBodyLimit256KiB) {
			return
		}
		preview, err := h.restorer.DryRun(id)
		if trashRestoreError(w, err) {
			return
		}
		writeMaintenanceJSON(w, 200, preview)
		return
	}
	request, ok := DecodeMaintenanceRequest[models.TrashRestoreRequest](w, r, MaintenanceJSONBodyLimit256KiB)
	if !ok {
		return
	}
	entry, operationID, err := h.restorer.Restore(id, request.ExpectedInventoryHash)
	if trashRestoreError(w, err) {
		return
	}
	writeMaintenanceJSON(w, 200, map[string]string{"entry_id": entry.ID, "original_id": entry.OriginalID, "original_relative_path": entry.OriginalRelativePath, "operation_id": operationID})
}

func trashRestoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, repositories.ErrMaintenanceID):
		writeMaintenanceInputError(w, 400, "invalid Trash entry ID")
	case errors.Is(err, repositories.ErrTrashMissing):
		writeMaintenanceInputError(w, 404, "Trash entry not found")
	case errors.Is(err, repositories.ErrTrashChanged), errors.Is(err, repositories.ErrTrashCollision):
		writeMaintenanceInputError(w, 409, "Trash restore conflict; inspect entry and operation record")
	default:
		writeMaintenanceInputError(w, 503, "Trash restore unavailable; inspect operation record")
	}
	return true
}

func parseTrashListQuery(raw string) (services.TrashListQuery, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return services.TrashListQuery{}, err
	}
	query := services.TrashListQuery{Limit: 50}
	for key, list := range values {
		if len(list) != 1 {
			return services.TrashListQuery{}, errors.New("duplicate query field")
		}
		switch key {
		case "type":
			query.Type = list[0]
		case "offset":
			query.Offset, err = strconv.Atoi(list[0])
			if err != nil {
				return services.TrashListQuery{}, err
			}
		case "limit":
			query.Limit, err = strconv.Atoi(list[0])
			if err != nil {
				return services.TrashListQuery{}, err
			}
		default:
			return services.TrashListQuery{}, errors.New("unknown query field")
		}
	}
	if query.Offset < 0 || query.Limit < 1 || query.Limit > 100 || !validTrashQueryType(query.Type) {
		return services.TrashListQuery{}, errors.New("invalid query value")
	}
	return query, nil
}

func validTrashQueryType(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func trashViewOf(entry models.TrashEntry) trashEntryView {
	return trashEntryView{EntryID: safeMaintenanceToken(entry.ID), Status: safeMaintenanceToken(entry.Status), TargetType: safeMaintenanceToken(entry.TargetType), OriginalID: safeMaintenanceToken(entry.OriginalID), OriginalRelativePath: safeTrashRelativePath(entry.OriginalRelativePath), DeletedAt: entry.DeletedAt, EligibleAfter: entry.EligibleAfter, LastCheckedAt: entry.LastCheckedAt, FinalDeletedAt: entry.FinalDeletedAt, FinalDeleteFilesDeleted: entry.FinalDeleteFilesDeleted, FinalDeleteFilesTotal: entry.FinalDeleteFilesTotal, Reason: safeMaintenanceError(entry.Reason), OperationType: safeMaintenanceToken(entry.OperationType), OperationID: safeMaintenanceToken(entry.OperationID), RestoreAvailable: entry.RestoreAvailable}
}

func safeTrashRelativePath(path string) string {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\\:") {
		return ""
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	return path
}
