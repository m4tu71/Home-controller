package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

type trashRestoreHandlerFixture struct {
	preview repositories.TrashRestorePreview
	entry   models.TrashEntry
	err     error
	calls   int
}

func (f *trashRestoreHandlerFixture) DryRun(string) (repositories.TrashRestorePreview, error) {
	f.calls++
	return f.preview, f.err
}
func (f *trashRestoreHandlerFixture) Restore(string, string) (models.TrashEntry, string, error) {
	f.calls++
	return f.entry, "restore_1", f.err
}

func TestTrashRestoreHTTPInput(t *testing.T) {
	valid := `{"expected_inventory_hash":"sha256:abc","confirm":true}`
	for _, tc := range []struct {
		name, path, body, contentType string
		want                          int
		calls                         int
	}{
		{"dry run", "/entry_1/restore/dry-run", "", "", 200, 1},
		{"dry run object", "/entry_1/restore/dry-run", "{}", "", 400, 0},
		{"dry run whitespace", "/entry_1/restore/dry-run", " ", "", 400, 0},
		{"dry run oversize", "/entry_1/restore/dry-run", strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), "", 413, 0},
		{"apply", "/entry_1/restore", valid, "application/json", 200, 1},
		{"wrong type", "/entry_1/restore", valid, "text/plain", 415, 0},
		{"missing confirm", "/entry_1/restore", `{"expected_inventory_hash":"sha256:abc"}`, "application/json", 400, 0},
		{"missing hash", "/entry_1/restore", `{"confirm":true}`, "application/json", 400, 0},
		{"unknown path", "/entry_1/restore", `{"expected_inventory_hash":"sha256:abc","confirm":true,"path":"x"}`, "application/json", 400, 0},
		{"trailing", "/entry_1/restore", valid + ` {}`, "application/json", 400, 0},
		{"apply oversize", "/entry_1/restore", strings.Repeat("x", int(MaintenanceJSONBodyLimit256KiB)+1), "application/json", 413, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &trashRestoreHandlerFixture{preview: repositories.TrashRestorePreview{EntryID: "entry_1", ExpectedInventoryHash: "sha256:abc", RestoreAvailable: true}, entry: models.TrashEntry{ID: "entry_1", OriginalID: "room_1", OriginalRelativePath: "rooms/room_1"}}
			h := &TrashReadHandler{restorer: f}
			r := httptest.NewRequest(http.MethodPost, trashReadPath+tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || f.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, f.calls, w.Body.String())
			}
			if tc.want == 200 && !json.Valid(w.Body.Bytes()) {
				t.Fatal("invalid response JSON")
			}
		})
	}
}

func TestTrashRestoreHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{{repositories.ErrTrashMissing, 404}, {repositories.ErrTrashChanged, 409}, {repositories.ErrTrashCorrupt, 503}} {
		f := &trashRestoreHandlerFixture{err: tc.err}
		h := &TrashReadHandler{restorer: f}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, trashReadPath+"/entry_1/restore/dry-run", nil))
		if w.Code != tc.want {
			t.Fatalf("err=%v status=%d", tc.err, w.Code)
		}
	}
}
