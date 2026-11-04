package repositories

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"backend/models"
)

func maintenanceFixture(t *testing.T) (*MaintenanceState, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "state")
	for _, dir := range []string{root, filepath.Join(root, "plans"), filepath.Join(root, "operations")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewMaintenanceState(MaintenanceStateConfig{Dir: root, RuntimeDataDir: filepath.Join(base, "data")})
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestMaintenancePlanPersistence(t *testing.T) {
	s, root := maintenanceFixture(t)
	p := json.RawMessage(`{"version":1,"opaque":{"x":2}}`)
	if err := s.Plans().Create("plan_1", p); err != nil {
		t.Fatal(err)
	}
	if err := s.Plans().Create("plan_1", p); !errors.Is(err, ErrMaintenanceDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.Plans().Load("missing"); !errors.Is(err, ErrMaintenanceMissing) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Plans().Create("../escape", p); !errors.Is(err, ErrMaintenanceID) {
		t.Fatalf("ID: %v", err)
	}
	restarted, err := NewMaintenanceState(MaintenanceStateConfig{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Plans().Load("plan_1")
	if err != nil || !json.Valid(got) {
		t.Fatalf("readback: %s %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, "plans", "plan_1.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode: %v %v", info, err)
	}
}

func TestMaintenanceConcurrentDuplicate(t *testing.T) {
	s, _ := maintenanceFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Plans().Create("same", json.RawMessage(`{"version":1}`)) }()
	}
	wg.Wait()
	close(results)
	success, duplicate := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrMaintenanceDuplicate) {
			duplicate++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || duplicate != 15 {
		t.Fatalf("success=%d duplicate=%d", success, duplicate)
	}
}

func TestMaintenanceConcurrentOperationPlanClaim(t *testing.T) {
	s, _ := maintenanceFixture(t)
	start := make(chan struct{})
	results := make(chan struct {
		id  string
		err error
	}, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"op_a", "op_b"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v := models.MaintenanceOperation{Version: 1, ID: id, Revision: 1, StartedAt: time.Now().UTC(), Target: "migration", PlanID: "plan_shared", PlanDigest: "sha256:plan", Status: "prepared"}
			results <- struct {
				id  string
				err error
			}{id: id, err: s.Operations().Create(v)}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	created, claimed := 0, 0
	for result := range results {
		if result.err == nil {
			created++
			winner = result.id
		} else if errors.Is(result.err, ErrMaintenancePlanClaimed) {
			claimed++
		} else {
			t.Fatalf("operation create %s: %v", result.id, result.err)
		}
	}
	if created != 1 || claimed != 1 {
		t.Fatalf("created=%d plan-claimed=%d", created, claimed)
	}
	if err := s.Operations().HasExclusivePlanClaim("plan_shared", winner); err != nil {
		t.Fatalf("winning operation claim: %v", err)
	}
	loser := "op_a"
	if loser == winner {
		loser = "op_b"
	}
	if err := s.Operations().HasExclusivePlanClaim("plan_shared", loser); !errors.Is(err, ErrMaintenancePlanClaimed) {
		t.Fatalf("losing operation claim = %v, want plan-claimed", err)
	}
}

func TestMigrationClaimReleasedOnlyAfterVerifiedRollback(t *testing.T) {
	s, _ := maintenanceFixture(t)
	path := "rooms/bedroom/devices/roomlight/adapters/ir/commands/on_ch3.json"
	base := time.Now().UTC().Add(-time.Minute)
	completed := base.Add(time.Second)
	first := models.MaintenanceOperation{Version: 1, ID: "migration_first", Revision: 1, StartedAt: base, CompletedAt: &completed, Target: "migration", Type: "migration", PlanID: "plan_first", PlanDigest: "sha256:first", ClaimedPaths: []string{path}, Status: "verified"}
	if err := s.Operations().Create(first); err != nil {
		t.Fatal(err)
	}
	next := models.MaintenanceOperation{Version: 1, ID: "migration_next", Revision: 1, StartedAt: base.Add(4 * time.Second), Target: "migration", Type: "migration", PlanID: "plan_next", PlanDigest: "sha256:next", ClaimedPaths: []string{path}, Status: "prepared"}
	if err := s.Operations().Create(next); !errors.Is(err, ErrMaintenanceTargetClaimed) {
		t.Fatalf("without rollback err=%v", err)
	}
	rollbackCompleted := base.Add(3 * time.Second)
	rollback := models.MaintenanceOperation{Version: 1, ID: "rollback_first", Revision: 1, StartedAt: base.Add(2 * time.Second), CompletedAt: &rollbackCompleted, Target: "migration_rollback", Type: "migration_rollback", PlanID: "plan_rollback", PlanDigest: "sha256:rollback", ClaimedPaths: []string{path}, Status: "verified"}
	if err := s.Operations().Create(rollback); err != nil {
		t.Fatal(err)
	}
	if err := s.Operations().Create(next); err != nil {
		t.Fatalf("after verified rollback err=%v", err)
	}
	third := next
	third.ID, third.PlanID = "migration_third", "plan_third"
	if err := s.Operations().Create(third); !errors.Is(err, ErrMaintenanceTargetClaimed) {
		t.Fatalf("unrolled second claim err=%v", err)
	}
}

func TestMaintenanceOperationRevision(t *testing.T) {
	s, root := maintenanceFixture(t)
	v := models.MaintenanceOperation{Version: 1, ID: "op_1", Revision: 1, StartedAt: time.Now().UTC(), Target: "room", Status: "prepared", Actions: []models.MaintenanceOperationAction{{ID: "a", Action: "write", Status: "pending"}}}
	if err := s.Operations().Create(v); err != nil {
		t.Fatal(err)
	}
	if err := s.Operations().Create(v); !errors.Is(err, ErrMaintenanceDuplicate) {
		t.Fatal(err)
	}
	v.Revision = 2
	v.ApplyBackupID = "backup_2"
	v.Status = "applying"
	if err := s.Operations().Update(v); err != nil {
		t.Fatal(err)
	}
	if err := s.Operations().Update(v); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatalf("conflict: %v", err)
	}
	restarted, err := NewMaintenanceState(MaintenanceStateConfig{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Operations().Load("op_1")
	if err != nil || got.Revision != 2 || got.ApplyBackupID != "backup_2" {
		t.Fatalf("readback: %+v %v", got, err)
	}
}

func TestMaintenanceOperationPlanBindingIsImmutable(t *testing.T) {
	s, _ := maintenanceFixture(t)
	v := models.MaintenanceOperation{Version: 1, ID: "op_immutable", Revision: 1, StartedAt: time.Now().UTC(), Target: "migration", PlanID: "plan_original", PlanDigest: "sha256:original", Status: "prepared"}
	if err := s.Operations().Create(v); err != nil {
		t.Fatal(err)
	}
	v.Revision = 2
	v.PlanID = "plan_replaced"
	v.PlanDigest = "sha256:replaced"
	if err := s.Operations().Update(v); !errors.Is(err, ErrMaintenanceConflict) {
		t.Fatalf("plan binding update = %v, want conflict", err)
	}
}

func TestMaintenanceOperationMissingAndCorrupt(t *testing.T) {
	s, root := maintenanceFixture(t)
	if _, err := s.Operations().Load("missing"); !errors.Is(err, ErrMaintenanceMissing) {
		t.Fatalf("missing operation: %v", err)
	}
	path := filepath.Join(root, "operations", "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Operations().Load("broken"); !errors.Is(err, ErrMaintenanceCorrupt) {
		t.Fatalf("corrupt operation JSON: %v", err)
	}
}

func TestMaintenanceStateFailures(t *testing.T) {
	s, root := maintenanceFixture(t)
	if _, err := NewMaintenanceState(MaintenanceStateConfig{Dir: root, RuntimeDataDir: filepath.Dir(root)}); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("overlap: %v", err)
	}
	if _, err := NewMaintenanceState(MaintenanceStateConfig{Dir: "relative"}); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("relative: %v", err)
	}
	link := filepath.Join(filepath.Dir(root), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMaintenanceState(MaintenanceStateConfig{Dir: link}); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("symlink: %v", err)
	}
	path := filepath.Join(root, "plans", "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plans().Load("bad"); !errors.Is(err, ErrMaintenanceCorrupt) {
		t.Fatalf("corrupt: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plans().Load("bad"); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("file mode: %v", err)
	}
	if err := os.Chmod(filepath.Join(root, "plans"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plans().Load("bad"); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("dir mode: %v", err)
	}
	if err := os.Chmod(filepath.Join(root, "plans"), 0700); err != nil {
		t.Fatal(err)
	}
	s.uid++
	if _, err := s.Plans().Load("bad"); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("owner: %v", err)
	}
}

func TestMaintenanceSymlinkAndAtomicFailure(t *testing.T) {
	s, root := maintenanceFixture(t)
	path := filepath.Join(root, "plans", "linked.json")
	if err := os.Symlink(filepath.Join(root, "other"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plans().Load("linked"); !errors.Is(err, ErrMaintenanceState) {
		t.Fatalf("file symlink: %v", err)
	}
	writeErr := errors.New("injected rename failure")
	s.writer = &AtomicJSONWriter{beforeStage: func(stage AtomicWriteStage) error {
		if stage == AtomicWriteRename {
			return writeErr
		}
		return nil
	}}
	if err := s.Plans().Create("failed", json.RawMessage(`{"version":1}`)); !errors.Is(err, writeErr) {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.Plans().Load("failed"); !errors.Is(err, ErrMaintenanceMissing) {
		t.Fatalf("failed write visible: %v", err)
	}
}

func TestMaintenanceConcurrentOperationUpdate(t *testing.T) {
	s, _ := maintenanceFixture(t)
	v := models.MaintenanceOperation{Version: 1, ID: "op", Revision: 1, StartedAt: time.Now().UTC(), Target: "target", Status: "prepared"}
	if err := s.Operations().Create(v); err != nil {
		t.Fatal(err)
	}
	v.Revision = 2
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Operations().Update(v) }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrMaintenanceConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestMaintenanceOperationListFailsClosedOnCorruptRecord(t *testing.T) {
	s, root := maintenanceFixture(t)
	v := models.MaintenanceOperation{Version: 1, ID: "op", Revision: 1, StartedAt: time.Now().UTC(), Target: "room", Status: "prepared"}
	if err := s.Operations().Create(v); err != nil {
		t.Fatal(err)
	}
	list, err := s.Operations().List()
	if err != nil || len(list) != 1 || list[0].ID != "op" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := os.WriteFile(filepath.Join(root, "operations", "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Operations().List(); !errors.Is(err, ErrMaintenanceCorrupt) {
		t.Fatalf("corrupt list: %v", err)
	}
}
