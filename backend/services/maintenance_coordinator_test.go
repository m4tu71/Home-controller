package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"backend/models"
	"backend/repositories"
)

type memoryMaintenanceStore struct {
	record       models.MaintenanceOperation
	snapshots    []models.MaintenanceOperation
	failCreate   bool
	failUpdateAt int
	updates      int
}

func cloneOperation(v models.MaintenanceOperation) models.MaintenanceOperation {
	data, _ := json.Marshal(v)
	var copy models.MaintenanceOperation
	_ = json.Unmarshal(data, &copy)
	return copy
}

func (s *memoryMaintenanceStore) Create(v models.MaintenanceOperation) error {
	if s.failCreate {
		return errors.New("injected create failure")
	}
	s.record = cloneOperation(v)
	s.snapshots = append(s.snapshots, s.record)
	return nil
}

func (s *memoryMaintenanceStore) Update(v models.MaintenanceOperation) error {
	s.updates++
	if s.updates == s.failUpdateAt {
		return errors.New("injected update failure")
	}
	if v.Revision != s.record.Revision+1 {
		return errors.New("invalid revision")
	}
	s.record = cloneOperation(v)
	s.snapshots = append(s.snapshots, s.record)
	return nil
}

func maintenanceWork(mutate func() error, readback func() (json.RawMessage, error)) MaintenanceActionWork {
	return MaintenanceActionWork{ID: "a", Action: "write", BeforeState: json.RawMessage(`{"exists":false}`), IntendedState: json.RawMessage(`{"exists":true}`), Mutate: mutate, Readback: readback}
}

func TestMaintenanceTransitions(t *testing.T) {
	validOperations := [][2]MaintenanceOperationStatus{{MaintenancePrepared, MaintenanceApplying}, {MaintenanceApplying, MaintenanceApplied}, {MaintenanceApplied, MaintenanceVerifying}, {MaintenanceVerifying, MaintenanceVerified}, {MaintenanceApplying, MaintenancePartialFailure}, {MaintenanceVerifying, MaintenanceVerificationFailed}}
	for _, pair := range validOperations {
		if err := ValidateMaintenanceOperationTransition(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]MaintenanceOperationStatus{{MaintenancePrepared, MaintenanceVerified}, {MaintenanceApplying, MaintenanceVerifying}, {MaintenanceVerified, MaintenanceApplying}, {MaintenancePartialFailure, MaintenanceApplying}} {
		if err := ValidateMaintenanceOperationTransition(pair[0], pair[1]); err == nil {
			t.Fatalf("allowed illegal transition %q -> %q", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]MaintenanceActionStatus{{MaintenanceActionPending, MaintenanceActionApplying}, {MaintenanceActionApplying, MaintenanceActionApplied}, {MaintenanceActionApplied, MaintenanceActionVerified}, {MaintenanceActionApplying, MaintenanceActionFailed}, {MaintenanceActionApplied, MaintenanceActionFailed}} {
		if err := ValidateMaintenanceActionTransition(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateMaintenanceActionTransition(MaintenanceActionPending, MaintenanceActionVerified); err == nil {
		t.Fatal("allowed skipped action transition")
	}
}

func TestMaintenanceCoordinatorSuccessAndWriteAhead(t *testing.T) {
	s := &memoryMaintenanceStore{}
	mutated := false
	work := maintenanceWork(func() error {
		if s.record.Status != string(MaintenanceApplying) || s.record.Actions[0].Status != string(MaintenanceActionApplying) || len(s.record.Events) != 1 || s.record.Events[0].Kind != "pre_mutation" {
			t.Fatal("mutation without durable intent")
		}
		mutated = true
		return nil
	}, func() (json.RawMessage, error) {
		if !mutated || s.record.Actions[0].Status != string(MaintenanceActionApplied) {
			t.Fatal("readback before durable mutation result")
		}
		return json.RawMessage(`{"exists":true}`), nil
	})
	got, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != string(MaintenanceVerified) || got.CompletedAt == nil || got.Actions[0].Status != string(MaintenanceActionVerified) || len(got.Events) != 3 || got.Events[2].Kind != "post_readback" {
		t.Fatalf("record: %+v", got)
	}
	if s.record.Status != string(MaintenanceVerified) || s.record.CompletedAt == nil {
		t.Fatal("completion not durable")
	}
}

func TestMaintenanceCoordinatorPreMutationFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create bool
		update int
	}{{"create", true, 0}, {"applying", false, 1}, {"intent", false, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &memoryMaintenanceStore{failCreate: tc.create, failUpdateAt: tc.update}
			mutations := 0
			work := maintenanceWork(func() error { mutations++; return nil }, func() (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
			if _, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work}); err == nil {
				t.Fatal("expected persistence error")
			}
			if mutations != 0 {
				t.Fatal("mutated despite failed write-ahead")
			}
		})
	}
}

func TestMaintenanceCoordinatorPostMutationRecordFailure(t *testing.T) {
	s := &memoryMaintenanceStore{failUpdateAt: 3}
	mutations, readbacks := 0, 0
	work := maintenanceWork(func() error { mutations++; return nil }, func() (json.RawMessage, error) { readbacks++; return json.RawMessage(`{}`), nil })
	_, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work})
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || mutations != 1 || readbacks != 0 || s.record.Actions[0].Status != string(MaintenanceActionApplying) {
		t.Fatalf("err=%v mutations=%d readbacks=%d record=%+v", err, mutations, readbacks, s.record)
	}
}

func TestMaintenanceCoordinatorPartialActionStopsFollowingWork(t *testing.T) {
	s := &memoryMaintenanceStore{}
	second := 0
	first := maintenanceWork(func() error { return errors.New("partial mutation") }, func() (json.RawMessage, error) { t.Fatal("unexpected readback"); return nil, nil })
	later := maintenanceWork(func() error { second++; return nil }, func() (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	later.ID = "b"
	_, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{first, later})
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || second != 0 || s.record.Status != string(MaintenancePartialFailure) || s.record.Actions[0].Status != string(MaintenanceActionFailed) || s.record.Actions[1].Status != string(MaintenanceActionPending) || s.record.CompletedAt == nil {
		t.Fatalf("err=%v second=%d record=%+v", err, second, s.record)
	}
}

func TestMaintenanceCoordinatorReadbackFailure(t *testing.T) {
	s := &memoryMaintenanceStore{}
	work := maintenanceWork(func() error { return nil }, func() (json.RawMessage, error) { return nil, errors.New("readback failed") })
	_, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work})
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || s.record.Status != string(MaintenanceVerificationFailed) || s.record.Actions[0].Status != string(MaintenanceActionFailed) {
		t.Fatalf("err=%v record=%+v", err, s.record)
	}
}

func TestMaintenanceCoordinatorReadbackRecordFailure(t *testing.T) {
	s := &memoryMaintenanceStore{failUpdateAt: 6}
	work := maintenanceWork(func() error { return nil }, func() (json.RawMessage, error) { return json.RawMessage(`{"verified":true}`), nil })
	_, err := NewMaintenanceCoordinator(s).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work})
	if !errors.Is(err, ErrMaintenanceOutcomeUncertain) || s.record.Actions[0].Status != string(MaintenanceActionApplied) {
		t.Fatalf("err=%v record=%+v", err, s.record)
	}
}

func TestMaintenanceCoordinatorRepositoryReadback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	for _, dir := range []string{root, filepath.Join(root, "plans"), filepath.Join(root, "operations")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	work := maintenanceWork(func() error { return nil }, func() (json.RawMessage, error) { return json.RawMessage(`{"verified":true}`), nil })
	if _, err := NewMaintenanceCoordinator(state.Operations()).Run(models.MaintenanceOperation{ID: "op", Target: "room"}, []MaintenanceActionWork{work}); err != nil {
		t.Fatal(err)
	}
	restarted, err := repositories.NewMaintenanceState(repositories.MaintenanceStateConfig{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Operations().Load("op")
	if err != nil || got.Status != string(MaintenanceVerified) || len(got.Events) != 3 || got.Events[0].Kind != "pre_mutation" || got.Events[2].Kind != "post_readback" {
		t.Fatalf("persisted record: %+v %v", got, err)
	}
}
