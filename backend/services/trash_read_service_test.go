package services

import (
	"errors"
	"testing"

	"backend/models"
	"backend/repositories"
)

type trashReadFixture struct {
	entries []models.TrashEntry
	err     error
}

func (f trashReadFixture) List() ([]models.TrashEntry, error) { return f.entries, f.err }
func (f trashReadFixture) Load(id string) (models.TrashEntry, error) {
	if f.err != nil {
		return models.TrashEntry{}, f.err
	}
	for _, entry := range f.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return models.TrashEntry{}, repositories.ErrTrashMissing
}

func TestTrashReadServiceFilterAndPagination(t *testing.T) {
	store := trashReadFixture{entries: []models.TrashEntry{{ID: "a", TargetType: "room"}, {ID: "b", TargetType: "device"}, {ID: "c", TargetType: "room"}}}
	s := NewTrashReadService(store)
	page, err := s.List(TrashListQuery{Type: "room", Offset: 1, Limit: 1})
	if err != nil || page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].ID != "c" || page.Offset != 1 || page.Limit != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	page, err = s.List(TrashListQuery{Type: "room", Offset: int(^uint(0) >> 1), Limit: 100})
	if err != nil || page.Total != 2 || len(page.Entries) != 0 {
		t.Fatalf("past end: %+v %v", page, err)
	}
	entry, err := s.Detail("b")
	if err != nil || entry.TargetType != "device" {
		t.Fatalf("detail: %+v %v", entry, err)
	}
	if _, err := s.Detail("missing"); !errors.Is(err, repositories.ErrTrashMissing) {
		t.Fatalf("missing: %v", err)
	}
}

func TestTrashReadServiceInvalidAndStoreFailure(t *testing.T) {
	s := NewTrashReadService(trashReadFixture{err: repositories.ErrTrashCorrupt})
	for _, query := range []TrashListQuery{{Limit: 0}, {Limit: 101}, {Limit: 1, Offset: -1}, {Limit: 1, Type: "/private"}} {
		if _, err := s.List(query); err == nil {
			t.Fatalf("accepted invalid query: %+v", query)
		}
	}
	if _, err := s.List(TrashListQuery{Limit: 50}); !errors.Is(err, repositories.ErrTrashCorrupt) {
		t.Fatalf("store error: %v", err)
	}
}
