package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"backend/repositories"
	"backend/services"
)

type maintenanceBackupFixture struct {
	preview repositories.BackupRestorePreview
	result  repositories.BackupRestoreResult
	err     error
}

type maintenanceFinalDeleteFixture struct {
	confirmed string
	called    bool
}

func (f *maintenanceFinalDeleteFixture) DryRun(id string) (services.TrashFinalDeletePreview, error) {
	return services.TrashFinalDeletePreview{EntryID: id, Eligible: true, PlanID: "plan_1"}, nil
}
func (f *maintenanceFinalDeleteFixture) Apply(planID, confirmed string) (repositories.TrashFinalDeleteResult, error) {
	f.called = true
	f.confirmed = confirmed
	return repositories.TrashFinalDeleteResult{EntryID: confirmed, PlanID: planID, Status: "final_deleted"}, nil
}

func TestRunTrashFinalDeleteCLI(t *testing.T) {
	service := &maintenanceFinalDeleteFixture{}
	var out bytes.Buffer
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete-dry-run", "--entry-id", "entry_1"}, service, nil, &out); err != nil || !strings.Contains(out.String(), `"plan_id": "plan_1"`) {
		t.Fatalf("dry-run: %s %v", out.String(), err)
	}
	out.Reset()
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete", "--plan-id", "plan_1"}, service, nil, &out); err == nil || service.called {
		t.Fatalf("non-TTY apply accepted: %v", err)
	}
	out.Reset()
	if err := runTrashFinalDeleteCLI([]string{"trash-final-delete", "--plan-id", "plan_1"}, service, bytes.NewBufferString("entry_1\n"), &out); err != nil || !service.called || service.confirmed != "entry_1" || !strings.Contains(out.String(), `"status": "final_deleted"`) {
		t.Fatalf("apply: %s %v", out.String(), err)
	}
	for _, args := range [][]string{{"trash-final-delete"}, {"trash-final-delete", "--entry-id", "entry_1"}, {"trash-final-delete-dry-run", "--plan-id", "plan_1"}} {
		if err := runTrashFinalDeleteCLI(args, service, bytes.NewBufferString("entry_1\n"), &out); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}

func (f maintenanceBackupFixture) RestoreDryRun(string) (repositories.BackupRestorePreview, error) {
	return f.preview, f.err
}
func (f maintenanceBackupFixture) RestoreRehearsal(string) (repositories.BackupRestoreResult, error) {
	return f.result, f.err
}

func TestRunMaintenanceCLI(t *testing.T) {
	var out bytes.Buffer
	service := maintenanceBackupFixture{preview: repositories.BackupRestorePreview{BackupID: "backup_1", CanRehearse: true}}
	if err := runMaintenanceCLI([]string{"restore-dry-run", "backup_1"}, service, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"can_rehearse": true`) {
		t.Fatalf("output=%s", out.String())
	}
	out.Reset()
	service.result = repositories.BackupRestoreResult{BackupID: "backup_1", State: "rehearsal_verified"}
	if err := runMaintenanceCLI([]string{"restore-rehearsal", "backup_1"}, service, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"state": "rehearsal_verified"`) {
		t.Fatalf("output=%s", out.String())
	}
	if err := runMaintenanceCLI([]string{"unknown", "backup_1"}, service, &out); err == nil {
		t.Fatal("unknown command accepted")
	}
	if err := runMaintenanceCLI([]string{"restore-dry-run"}, service, &out); err == nil {
		t.Fatal("missing argument accepted")
	}
	service.err = errors.New("injected backup failure")
	if err := runMaintenanceCLI([]string{"restore-rehearsal", "backup_1"}, service, &out); err == nil {
		t.Fatal("service error discarded")
	}
}
