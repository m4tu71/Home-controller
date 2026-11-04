package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/models"
)

var ErrMaintenanceOutcomeUncertain = errors.New("maintenance outcome uncertain; inspect record and filesystem before recovery")

// MaintenanceOperationStore is intentionally narrow. Mutation callbacks are
// invoked only by Run, after the prepared record and per-action intent persist.
type MaintenanceOperationStore interface {
	Create(models.MaintenanceOperation) error
	Update(models.MaintenanceOperation) error
}

type MaintenanceActionWork struct {
	ID            string
	Action        string
	BeforeState   json.RawMessage
	IntendedState json.RawMessage
	Mutate        func() error
	Readback      func() (json.RawMessage, error)
}

type MaintenanceCoordinator struct {
	store MaintenanceOperationStore
	now   func() time.Time
}

func NewMaintenanceCoordinator(store MaintenanceOperationStore) *MaintenanceCoordinator {
	return &MaintenanceCoordinator{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Run starts a new operation. It neither resumes nor automatically retries a
// partial operation. Callers must establish backup and plan safety separately.
func (c *MaintenanceCoordinator) Run(draft models.MaintenanceOperation, work []MaintenanceActionWork) (models.MaintenanceOperation, error) {
	return c.RunWithBeforeMutation(draft, work, nil)
}

// RunWithBeforeMutation permits a caller to create and durably associate an
// apply backup after the operation is claimed/applying but before any action
// enters its pre-mutation checkpoint. The callback returns the reloaded record
// because backup association itself increments its revision.
func (c *MaintenanceCoordinator) RunWithBeforeMutation(draft models.MaintenanceOperation, work []MaintenanceActionWork, beforeMutation func(string) (models.MaintenanceOperation, error)) (models.MaintenanceOperation, error) {
	if c == nil || c.store == nil || c.now == nil {
		return draft, errors.New("maintenance coordinator is not configured")
	}
	if draft.ID == "" || draft.Target == "" || draft.Revision != 0 || draft.Status != "" || len(draft.Actions) != 0 || len(draft.Events) != 0 || len(work) == 0 {
		return draft, errors.New("invalid maintenance operation draft")
	}
	seen := make(map[string]bool, len(work))
	for _, w := range work {
		if w.ID == "" || seen[w.ID] || w.Action == "" || !json.Valid(w.BeforeState) || !json.Valid(w.IntendedState) || w.Mutate == nil || w.Readback == nil {
			return draft, errors.New("invalid maintenance action work")
		}
		seen[w.ID] = true
		draft.Actions = append(draft.Actions, models.MaintenanceOperationAction{ID: w.ID, Action: w.Action, Status: string(MaintenanceActionPending), BeforeState: append(json.RawMessage(nil), w.BeforeState...), IntendedState: append(json.RawMessage(nil), w.IntendedState...)})
	}
	draft.Version = 1
	draft.Revision = 1
	draft.StartedAt = c.now()
	draft.Status = string(MaintenancePrepared)
	if err := c.store.Create(draft); err != nil {
		return draft, err
	}
	if err := c.operationStatus(&draft, MaintenanceApplying); err != nil {
		return draft, err
	}
	if beforeMutation != nil {
		updated, err := beforeMutation(draft.ID)
		if updated.ID == draft.ID && updated.PlanID == draft.PlanID && updated.PlanDigest == draft.PlanDigest && updated.Status == string(MaintenanceApplying) && updated.Revision >= draft.Revision && len(updated.Actions) == len(draft.Actions) {
			draft = updated
		} else {
			return draft, fmt.Errorf("%w: backup association record unavailable", ErrMaintenanceOutcomeUncertain)
		}
		if err != nil {
			draft.Error = err.Error()
			draft.Result = string(MaintenancePartialFailure)
			draft.Events = append(draft.Events, models.MaintenanceOperationEvent{At: c.now(), Kind: "pre_mutation_error", Error: err.Error()})
			done := c.now()
			draft.CompletedAt = &done
			if persistErr := c.operationStatus(&draft, MaintenancePartialFailure); persistErr != nil {
				return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, errors.Join(err, persistErr))
			}
			return draft, err
		}
	}

	for i, w := range work {
		a := &draft.Actions[i]
		if err := ValidateMaintenanceActionTransition(MaintenanceActionStatus(a.Status), MaintenanceActionApplying); err != nil {
			return draft, err
		}
		a.Status = string(MaintenanceActionApplying)
		a.BeforeState = append(json.RawMessage(nil), w.BeforeState...)
		a.IntendedState = append(json.RawMessage(nil), w.IntendedState...)
		draft.Events = append(draft.Events, models.MaintenanceOperationEvent{At: c.now(), ActionID: w.ID, Kind: "pre_mutation", BeforeState: a.BeforeState, IntendedState: a.IntendedState})
		if err := c.persist(&draft); err != nil {
			return draft, err
		}

		if err := w.Mutate(); err != nil {
			return draft, c.failAction(&draft, i, err, MaintenancePartialFailure)
		}
		if err := ValidateMaintenanceActionTransition(MaintenanceActionStatus(a.Status), MaintenanceActionApplied); err != nil {
			return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
		}
		a.Status = string(MaintenanceActionApplied)
		draft.Events = append(draft.Events, models.MaintenanceOperationEvent{At: c.now(), ActionID: w.ID, Kind: "mutation_returned"})
		if err := c.persist(&draft); err != nil {
			return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
		}

	}
	for _, status := range []MaintenanceOperationStatus{MaintenanceApplied, MaintenanceVerifying} {
		if err := c.operationStatus(&draft, status); err != nil {
			return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
		}
	}
	for i, w := range work {
		a := &draft.Actions[i]
		after, err := w.Readback()
		if err != nil || !json.Valid(after) {
			if err == nil {
				err = errors.New("readback returned invalid JSON")
			}
			return draft, c.failAction(&draft, i, err, MaintenanceVerificationFailed)
		}
		if err := ValidateMaintenanceActionTransition(MaintenanceActionStatus(a.Status), MaintenanceActionVerified); err != nil {
			return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
		}
		a.Status = string(MaintenanceActionVerified)
		a.AfterState = append(json.RawMessage(nil), after...)
		draft.Events = append(draft.Events, models.MaintenanceOperationEvent{At: c.now(), ActionID: w.ID, Kind: "post_readback", AfterState: a.AfterState})
		if err := c.persist(&draft); err != nil {
			return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
		}
	}
	done := c.now()
	draft.CompletedAt = &done
	draft.Result = "verified"
	if err := c.operationStatus(&draft, MaintenanceVerified); err != nil {
		return draft, fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, err)
	}
	return draft, nil
}

func (c *MaintenanceCoordinator) operationStatus(v *models.MaintenanceOperation, next MaintenanceOperationStatus) error {
	if err := ValidateMaintenanceOperationTransition(MaintenanceOperationStatus(v.Status), next); err != nil {
		return err
	}
	v.Status = string(next)
	return c.persist(v)
}

func (c *MaintenanceCoordinator) persist(v *models.MaintenanceOperation) error {
	v.Revision++
	return c.store.Update(*v)
}

func (c *MaintenanceCoordinator) failAction(v *models.MaintenanceOperation, index int, cause error, terminal MaintenanceOperationStatus) error {
	a := &v.Actions[index]
	if err := ValidateMaintenanceActionTransition(MaintenanceActionStatus(a.Status), MaintenanceActionFailed); err != nil {
		return fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, errors.Join(cause, err))
	}
	a.Status = string(MaintenanceActionFailed)
	a.Error = cause.Error()
	v.Error = cause.Error()
	v.Result = string(terminal)
	v.Events = append(v.Events, models.MaintenanceOperationEvent{At: c.now(), ActionID: a.ID, Kind: "action_error", Error: cause.Error()})
	done := c.now()
	v.CompletedAt = &done
	if err := c.operationStatus(v, terminal); err != nil {
		return fmt.Errorf("%w: %v", ErrMaintenanceOutcomeUncertain, errors.Join(cause, err))
	}
	return fmt.Errorf("%w: %w", ErrMaintenanceOutcomeUncertain, cause)
}
