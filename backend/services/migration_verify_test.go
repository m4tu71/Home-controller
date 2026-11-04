package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
)

func verifyAppliedFixture(t *testing.T, format string) (cleanupApplyFixture, *MigrationVerifyService, models.MaintenanceOperation) {
	t.Helper()
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, format)
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	plans := NewMigrationPlanService(common)
	m := migrationDryRunRequest(format).Mappings[0]
	if format == "raw_median" {
		m.Decision.ApproveRawMedian = true
	}
	if format == "preset_command" {
		m.Decision.PicoCommand = "LIGHT_ON_CH3"
	}
	plan, err := NewMigrationDryRunService(f.root, plans).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{m}})
	if err != nil {
		t.Fatal(err)
	}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	op, err := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root).Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: []string{m.ActionID}, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	return f, NewMigrationVerifyService(plans, f.state.Operations(), f.root), op
}

func TestMigrationVerifyValidFormatsReadOnly(t *testing.T) {
	for _, format := range []string{"raw", "raw_samples", "raw_median", "preset_command"} {
		t.Run(format, func(t *testing.T) {
			f, s, op := verifyAppliedFixture(t, format)
			path := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Verify(models.MigrationVerifyRequest{OperationID: op.ID})
			if err != nil || result.Status != "verified" || len(result.Targets) != 1 || result.Targets[0].Status != "verified" || result.Targets[0].ExpectedSHA256 != result.Targets[0].ActualSHA256 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("verify changed runtime: %v", err)
			}
		})
	}
}

func TestMigrationVerifyReportsMissingHashParseAndFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
		reason string
	}{
		{"missing", func(t *testing.T, p string) {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}, "missing_target"},
		{"hash", func(t *testing.T, p string) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}, "hash_mismatch"},
		{"parse", func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}, "repository_readback_failed"},
		{"format", func(t *testing.T, p string) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var cmd models.IRCommand
			if json.Unmarshal(data, &cmd) != nil {
				t.Fatal("decode")
			}
			cmd.Format = models.OperationFormatRawSamples
			out, _ := json.Marshal(cmd)
			if err := os.WriteFile(p, out, 0600); err != nil {
				t.Fatal(err)
			}
		}, "format_mismatch"},
		{"count", func(t *testing.T, p string) { rewriteMigrationCommand(t, p, func(c *models.IRCommand) { c.Count++ }) }, "count_mismatch"},
		{"timings", func(t *testing.T, p string) {
			rewriteMigrationCommand(t, p, func(c *models.IRCommand) { c.TimingsUS[0]++ })
		}, "timings_or_captures_mismatch"},
		{"schema", func(t *testing.T, p string) { rewriteMigrationCommand(t, p, func(c *models.IRCommand) { c.Name = "" }) }, "invalid_schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, op := verifyAppliedFixture(t, "raw")
			p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
			tc.mutate(t, p)
			result, err := s.Verify(models.MigrationVerifyRequest{OperationID: op.ID})
			if err != nil || result.Status != "failed" || result.Targets[0].Reason != tc.reason {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func rewriteMigrationCommand(t *testing.T, p string, mutate func(*models.IRCommand)) {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var cmd models.IRCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		t.Fatal(err)
	}
	mutate(&cmd)
	out, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationVerifyRejectsMissingOrWrongOperation(t *testing.T) {
	f, s, op := verifyAppliedFixture(t, "raw")
	if _, err := s.Verify(models.MigrationVerifyRequest{OperationID: "migration_missing"}); !errors.Is(err, repositories.ErrMaintenanceMissing) {
		t.Fatalf("missing err=%v", err)
	}
	if _, err := s.Verify(models.MigrationVerifyRequest{}); !errors.Is(err, ErrMigrationVerifyInvalid) {
		t.Fatalf("invalid err=%v", err)
	}
	stored, err := f.state.Operations().Load(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Type = "cleanup"
	stored.Revision++
	if err := f.state.Operations().Update(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(models.MigrationVerifyRequest{OperationID: op.ID}); !errors.Is(err, ErrMigrationVerifyInvalid) {
		t.Fatalf("type err=%v", err)
	}
}

func TestMigrationVerifyPartialApplyReportsEachTarget(t *testing.T) {
	f := newCleanupApplyFixture(t)
	writeMigrationFixture(t, f.root, "raw")
	writeMigrationFixture(t, f.root, "raw_median")
	common := NewMaintenancePlanService(f.state.Plans(), f.backup)
	plans := NewMigrationPlanService(common)
	first := migrationDryRunRequest("raw").Mappings[0]
	second := migrationDryRunRequest("raw_median").Mappings[0]
	second.ActionID = "migrate_second"
	second.TargetID = "second"
	second.Decision.TargetID = "second"
	second.Decision.ApproveRawMedian = true
	plan, err := NewMigrationDryRunService(f.root, plans).Create(models.MigrationDryRunRequest{Mappings: []models.MigrationDryRunMapping{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{first.ActionID, second.ActionID}
	backup := NewBackupService(f.backup, f.state.Operations()).WithPlanService(common)
	rehearsal, err := backup.CreateRehearsal(models.BackupCreateRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreRehearsal(rehearsal.Bundle.ID); err != nil {
		t.Fatal(err)
	}
	apply := NewMigrationApplyService(plans, f.state.Operations(), backup, f.root)
	apply.writer.Writer = &migrationFailNthWriter{failAt: 2}
	op, err := apply.Apply(models.MigrationApplyRequest{PlanID: plan.PlanID, PlanDigest: plan.Digest, ActionIDs: ids, RehearsalBackupID: rehearsal.Bundle.ID, Confirm: true})
	if err == nil || op.Status != "partial_failure" {
		t.Fatalf("op=%+v err=%v", op, err)
	}
	result, err := NewMigrationVerifyService(plans, f.state.Operations(), f.root).Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || result.Status != "failed" || len(result.Targets) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	byID := map[string]models.MigrationVerifyTarget{}
	for _, v := range result.Targets {
		byID[v.ActionID] = v
	}
	if byID[first.ActionID].Status != "verified" || byID[second.ActionID].Reason != "missing_target" {
		t.Fatalf("targets=%+v", byID)
	}
}

func TestMigrationVerifyReportsSourceAndCaptureMismatch(t *testing.T) {
	f, s, op := verifyAppliedFixture(t, "raw_samples")
	p := filepath.Join(f.root, "rooms/r/devices/d/adapters/ir/commands/power.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var cmd models.IRCommand
	if json.Unmarshal(data, &cmd) != nil {
		t.Fatal("decode")
	}
	cmd.Captures[0].TimingsUS[0] = 300
	out, _ := json.Marshal(cmd)
	if err := os.WriteFile(p, out, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || result.Targets[0].Reason != "timings_or_captures_mismatch" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "ir-signals/raw_samples.json"), []byte(strings.Repeat("x", 3)), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = s.Verify(models.MigrationVerifyRequest{OperationID: op.ID})
	if err != nil || result.Targets[0].Reason != "source_changed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
