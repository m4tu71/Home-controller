package repositories

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"backend/models"
)

type MaintenanceOperationRepository struct{ state *MaintenanceState }

// List fails closed on any malformed record. Atomic-writer temp files are not
// records and are never returned or deleted here.
func (r *MaintenanceOperationRepository) List() ([]models.MaintenanceOperation, error) {
	var records []models.MaintenanceOperation
	err := r.state.withLock(func() error {
		var err error
		records, err = r.listLocked()
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].StartedAt.Equal(records[j].StartedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].StartedAt.After(records[j].StartedAt)
	})
	return records, nil
}

func validOperation(v models.MaintenanceOperation) bool {
	planFieldsValid := (v.PlanID == "" && v.PlanDigest == "") || (maintenanceIDPattern.MatchString(v.PlanID) && v.PlanDigest != "")
	if v.Type == "delete" {
		// Earlier delete records predate path claims. Keep them readable; newly
		// created ordinary DELETE operations always claim exactly one path.
		if len(v.ClaimedPaths) != 0 && (len(v.ClaimedPaths) != 1 || !validTrashRelative(v.ClaimedPaths[0]) || v.Target != v.ClaimedPaths[0]) {
			return false
		}
	} else if v.Type == "migration_rollback" || v.Type == "migration" && len(v.ClaimedPaths) > 0 {
		if len(v.ClaimedPaths) == 0 {
			return false
		}
		seen := map[string]bool{}
		for _, p := range v.ClaimedPaths {
			if !validTrashRelative(p) || seen[p] {
				return false
			}
			seen[p] = true
		}
	} else if len(v.ClaimedPaths) != 0 {
		return false
	}
	return v.Version > 0 && maintenanceIDPattern.MatchString(v.ID) && v.Revision > 0 && !v.StartedAt.IsZero() && v.Target != "" && v.Status != "" && planFieldsValid
}

func (r *MaintenanceOperationRepository) Create(v models.MaintenanceOperation) error {
	path, err := maintenancePath(r.state, "operations", v.ID)
	if err != nil {
		return err
	}
	if !validOperation(v) || v.Revision != 1 {
		return ErrMaintenanceCorrupt
	}
	return r.state.withLock(func() error {
		_, err := os.Lstat(path)
		if err == nil {
			return ErrMaintenanceDuplicate
		}
		if !os.IsNotExist(err) {
			return err
		}
		if v.PlanID != "" || len(v.ClaimedPaths) > 0 {
			records, err := r.listLocked()
			if err != nil {
				return err
			}
			for _, existing := range records {
				if v.PlanID != "" && existing.PlanID == v.PlanID {
					return ErrMaintenancePlanClaimed
				}
				if len(v.ClaimedPaths) > 0 && len(existing.ClaimedPaths) > 0 && (v.Type == existing.Type || v.Type == "delete" || existing.Type == "delete") {
					for _, path := range v.ClaimedPaths {
						for _, claimed := range existing.ClaimedPaths {
							if path == claimed {
								if v.Type == "migration" && migrationClaimRolledBack(existing, path, v.StartedAt, records) {
									continue
								}
								return ErrMaintenanceTargetClaimed
							}
						}
					}
				}
			}
		}
		return r.state.writer.WriteJSON(path, v, 0600)
	})
}

func migrationClaimRolledBack(existing models.MaintenanceOperation, target string, attemptStarted time.Time, records []models.MaintenanceOperation) bool {
	if existing.Type != "migration" || existing.Status != "verified" || existing.CompletedAt == nil {
		return false
	}
	for _, rollback := range records {
		if rollback.Type != "migration_rollback" || rollback.Status != "verified" || rollback.CompletedAt == nil || !rollback.CompletedAt.After(*existing.CompletedAt) || rollback.CompletedAt.After(attemptStarted) {
			continue
		}
		for _, claimed := range rollback.ClaimedPaths {
			if claimed == target {
				return true
			}
		}
	}
	return false
}

// HasExclusivePlanClaim verifies that this operation is the sole durable
// claimant for a plan. Create performs the claim atomically under the same
// state-root lock, so concurrent operation creation cannot pass together.
func (r *MaintenanceOperationRepository) HasExclusivePlanClaim(planID, operationID string) error {
	if !maintenanceIDPattern.MatchString(planID) || !maintenanceIDPattern.MatchString(operationID) {
		return ErrMaintenanceID
	}
	return r.state.withLock(func() error {
		records, err := r.listLocked()
		if err != nil {
			return err
		}
		claims := 0
		for _, record := range records {
			if record.PlanID != planID {
				continue
			}
			claims++
			if record.ID != operationID {
				return ErrMaintenancePlanClaimed
			}
		}
		if claims != 1 {
			return ErrMaintenancePlanClaimed
		}
		return nil
	})
}

func (r *MaintenanceOperationRepository) Load(id string) (models.MaintenanceOperation, error) {
	path, err := maintenancePath(r.state, "operations", id)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	var v models.MaintenanceOperation
	err = r.state.withLock(func() error {
		var err error
		v, err = r.load(path, id)
		return err
	})
	return v, err
}

// Update is compare-and-swap: callers pass the revision read previously plus one.
func (r *MaintenanceOperationRepository) Update(v models.MaintenanceOperation) error {
	path, err := maintenancePath(r.state, "operations", v.ID)
	if err != nil {
		return err
	}
	if !validOperation(v) || v.Revision < 2 {
		return ErrMaintenanceCorrupt
	}
	return r.state.withLock(func() error {
		old, err := r.load(path, v.ID)
		if err != nil {
			return err
		}
		if old.Revision+1 != v.Revision {
			return ErrMaintenanceConflict
		}
		if old.PlanID != v.PlanID || old.PlanDigest != v.PlanDigest || !sameClaimedPaths(old.ClaimedPaths, v.ClaimedPaths) {
			return ErrMaintenanceConflict
		}
		return r.state.writer.WriteJSON(path, v, 0600)
	})
}

func sameClaimedPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listLocked reads all operation records while the caller holds the state lock.
// It deliberately fails closed on unexpected entries and corrupt records.
func (r *MaintenanceOperationRepository) listLocked() ([]models.MaintenanceOperation, error) {
	entries, err := os.ReadDir(filepath.Join(r.state.config.Dir, "operations"))
	if err != nil {
		return nil, err
	}
	var records []models.MaintenanceOperation
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("%w: unexpected operation entry %s", ErrMaintenanceCorrupt, name)
		}
		id := strings.TrimSuffix(name, ".json")
		if !maintenanceIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: invalid operation filename %s", ErrMaintenanceCorrupt, name)
		}
		v, err := r.load(filepath.Join(r.state.config.Dir, "operations", name), id)
		if err != nil {
			return nil, err
		}
		records = append(records, v)
	}
	return records, nil
}

func (r *MaintenanceOperationRepository) load(path, id string) (models.MaintenanceOperation, error) {
	data, err := r.state.readFile(path)
	if err != nil {
		return models.MaintenanceOperation{}, err
	}
	var v models.MaintenanceOperation
	if len(data) > 16<<20 || json.Unmarshal(data, &v) != nil || !validOperation(v) || v.ID != id {
		return models.MaintenanceOperation{}, ErrMaintenanceCorrupt
	}
	return v, nil
}
