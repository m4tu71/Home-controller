package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type trashHandlerFixture struct {
	entry                models.TrashEntry
	listErr, errorDetail error
	empty                bool
}

func (f trashHandlerFixture) List(query services.TrashListQuery) (services.TrashListPage, error) {
	if f.listErr != nil {
		return services.TrashListPage{}, f.listErr
	}
	if f.empty {
		return services.TrashListPage{Entries: []models.TrashEntry{}, Total: 0, Offset: query.Offset, Limit: query.Limit}, nil
	}
	return services.TrashListPage{Entries: []models.TrashEntry{f.entry}, Total: 1, Offset: query.Offset, Limit: query.Limit}, nil
}

func TestTrashReadEmptyList(t *testing.T) {
	h := &TrashReadHandler{reader: trashHandlerFixture{empty: true}}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, trashReadPath, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"entries":[]`) || !strings.Contains(w.Body.String(), `"limit":50`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func (f trashHandlerFixture) Detail(id string) (models.TrashEntry, error) {
	if f.errorDetail != nil {
		return models.TrashEntry{}, f.errorDetail
	}
	if id != f.entry.ID {
		return models.TrashEntry{}, repositories.ErrTrashMissing
	}
	return f.entry, nil
}

func TestTrashReadListDetailAndPathRedaction(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	entry := models.TrashEntry{ID: "entry_1", Status: "sealed", TargetType: "room", OriginalID: "bedroom", OriginalRelativePath: "/private/internal/path", Reason: "deleted /private/internal/path", OperationType: "delete", OperationID: "op_1", DeletedAt: &now, RestoreAvailable: true, BeforeInventory: models.TrashInventory{Kind: "file", FileCount: 1, TotalSize: 5, SHA256: strings.Repeat("a", 64)}}
	h := &TrashReadHandler{reader: trashHandlerFixture{entry: entry}}
	for _, path := range []string{trashReadPath + "?type=room&offset=0&limit=10", trashReadPath + "/entry_1"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "/private/") {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, trashReadPath+"/entry_1", nil))
	if !strings.Contains(w.Body.String(), `"inventory"`) || strings.Contains(w.Body.String(), `"payload"`) {
		t.Fatal(w.Body.String())
	}
}

func TestTrashReadQueryAndRouteErrors(t *testing.T) {
	h := &TrashReadHandler{reader: trashHandlerFixture{entry: models.TrashEntry{ID: "entry_1"}}}
	for _, tc := range []struct {
		name, path, method string
		want               int
	}{
		{"missing", trashReadPath + "/missing", http.MethodGet, http.StatusNotFound},
		{"invalid ID", trashReadPath + "/%20", http.MethodGet, http.StatusBadRequest},
		{"restore unavailable", trashReadPath + "/entry_1/restore", http.MethodPost, http.StatusServiceUnavailable},
		{"method", trashReadPath, http.MethodPost, http.StatusMethodNotAllowed},
		{"unknown filter", trashReadPath + "?path=/private", http.MethodGet, http.StatusBadRequest},
		{"duplicate", trashReadPath + "?type=room&type=device", http.MethodGet, http.StatusBadRequest},
		{"negative", trashReadPath + "?offset=-1", http.MethodGet, http.StatusBadRequest},
		{"limit", trashReadPath + "?limit=101", http.MethodGet, http.StatusBadRequest},
		{"malformed", trashReadPath + "?type=%zz", http.MethodGet, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestTrashReadUnavailableAndCorrupt(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		handler    *TrashReadHandler
	}{
		{"unconfigured", trashReadPath, NewTrashReadHandler(nil)},
		{"corrupt list", trashReadPath, &TrashReadHandler{reader: trashHandlerFixture{listErr: repositories.ErrTrashCorrupt}}},
		{"corrupt detail", trashReadPath + "/entry_1", &TrashReadHandler{reader: trashHandlerFixture{errorDetail: repositories.ErrTrashCorrupt}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
