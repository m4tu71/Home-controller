package services

import (
	"errors"
	"fmt"

	"backend/models"
)

type TrashReadStore interface {
	List() ([]models.TrashEntry, error)
	Load(string) (models.TrashEntry, error)
}

type TrashListQuery struct {
	Type   string
	Offset int
	Limit  int
}

type TrashListPage struct {
	Entries []models.TrashEntry
	Total   int
	Offset  int
	Limit   int
}

type TrashReadService struct{ store TrashReadStore }

func NewTrashReadService(store TrashReadStore) *TrashReadService {
	return &TrashReadService{store: store}
}

func (s *TrashReadService) List(query TrashListQuery) (TrashListPage, error) {
	if s == nil || s.store == nil {
		return TrashListPage{}, errors.New("Trash history unavailable")
	}
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || !validTrashTypeFilter(query.Type) {
		return TrashListPage{}, fmt.Errorf("invalid Trash list query")
	}
	all, err := s.store.List()
	if err != nil {
		return TrashListPage{}, err
	}
	filtered := make([]models.TrashEntry, 0, len(all))
	for _, entry := range all {
		if query.Type == "" || entry.TargetType == query.Type {
			filtered = append(filtered, entry)
		}
	}
	page := TrashListPage{Entries: []models.TrashEntry{}, Total: len(filtered), Offset: query.Offset, Limit: query.Limit}
	if query.Offset >= len(filtered) {
		return page, nil
	}
	end := len(filtered)
	if query.Limit < len(filtered)-query.Offset {
		end = query.Offset + query.Limit
	}
	page.Entries = filtered[query.Offset:end]
	return page, nil
}

func (s *TrashReadService) Detail(id string) (models.TrashEntry, error) {
	if s == nil || s.store == nil {
		return models.TrashEntry{}, errors.New("Trash history unavailable")
	}
	return s.store.Load(id)
}

func validTrashTypeFilter(value string) bool {
	if value == "" {
		return true
	}
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
