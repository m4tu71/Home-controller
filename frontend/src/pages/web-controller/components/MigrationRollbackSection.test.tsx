import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { MigrationRollbackDryRunResult } from "../../../api/controller";
import { MigrationRollbackSection } from "./MigrationRollbackSection";

const operationID = "migration_operation_1";
const planID = "rollback_plan_1";
const eligiblePath = "rooms/living/devices/tv/adapters/ir/commands/power.json";
const modifiedPath = "rooms/living/devices/tv/adapters/ir/commands/volume.json";
const eligibleTarget = {
  action_id: "migrate_power",
  target_path: eligiblePath,
  apply_sha256: `sha256:${"a".repeat(64)}`,
  current_exists: true,
  current_kind: "file",
  current_sha256: `sha256:${"a".repeat(64)}`,
  eligible: true,
  reason: "eligible",
  expected_after_state: "target_moved_to_trash",
  dependencies: [{
    path: "rooms/living/room.json",
    kind: "json",
    role: "room",
    created_by_apply: true,
    shared_by_action_ids: ["migrate_power", "migrate_volume"],
  }],
};
const modifiedTarget = {
  action_id: "migrate_volume",
  target_path: modifiedPath,
  apply_sha256: `sha256:${"b".repeat(64)}`,
  current_exists: true,
  current_kind: "file",
  current_sha256: `sha256:${"c".repeat(64)}`,
  eligible: false,
  reason: "modified_target",
  expected_after_state: "target_unchanged",
  dependencies: [],
};

function rollbackDryRun(
  eligible = [eligibleTarget, modifiedTarget],
): MigrationRollbackDryRunResult {
  const ready = eligible.some((target) => target.eligible);
  return {
    ok: true,
    plan: {
      version: 1,
      plan_id: planID,
      operation_type: "migration_rollback",
      payload_schema_id: "migration_rollback.v1",
      payload: {
        migration_operation_id: operationID,
        migration_plan_id: "migration_plan_1",
        migration_plan_digest: `sha256:${"d".repeat(64)}`,
        apply_backup_id: "backup_original_apply",
        targets: eligible,
      },
      created_at: "2026-09-25T01:00:00Z",
      ready_to_apply: ready,
      backup_requirement: "apply",
      actions: eligible.map((target) => ({
        action_id: target.action_id,
        action: "rollback_target",
        selectable: target.eligible,
        destructive: target.eligible,
        ...(target.eligible ? {
          backup_sources: [target.target_path],
          backup_inventory_hash: `sha256:${"a".repeat(64)}`,
          backup_file_count: 1,
          backup_total_size: 128,
        } : {}),
      })),
      plan_digest: `sha256:${"e".repeat(64)}`,
    },
    targets: eligible,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

async function createPlan(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "rollback dry-run" }));
  await screen.findByText(planID);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MigrationRollbackSection", () => {
  it("shows apply/current hashes, human-change exclusions, backup reference and preserved dependencies", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(rollbackDryRun(), 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await createPlan(user);

    expect(screen.getByText("backup_original_apply")).toBeInTheDocument();
    expect(screen.getByText(/role apply \/ point_in_time_verified/)).toBeInTheDocument();
    expect(screen.getByText(/apply後に内容が変更された/)).toBeInTheDocument();
    expect(screen.getAllByText(eligibleTarget.apply_sha256)).toHaveLength(3);
    expect(screen.getByText(/Pre-rollback backup inventory/)).toBeInTheDocument();
    expect(screen.getByText(modifiedTarget.current_sha256)).toBeInTheDocument();
    expect(screen.getByText(/created_by_apply: yes; shared by: migrate_power, migrate_volume/)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /migrate_power/ })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /migrate_volume/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "確認してrollback（Trashへ移動）" })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/controller/maintenance/migration/rollback/dry-run",
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ operation_id: operationID }),
        cache: "no-store",
      }),
    );
  });

  it("has no apply path when dry-run has no eligible targets", async () => {
    const onlyModified = { ...modifiedTarget, action_id: "migrate_power" };
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(rollbackDryRun([onlyModified]), 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await createPlan(user);

    expect(screen.getByText("安全なrollback候補なし")).toBeInTheDocument();
    expect(screen.getByText(/安全な候補がありません/)).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: /migrate_power/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "確認してrollback（Trashへ移動）" })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("requires confirmation and sends only selected eligible targets, displaying Trash and rollback backup IDs", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(rollbackDryRun(), 201))
      .mockResolvedValueOnce(jsonResponse({
        ok: true,
        operation_id: "rollback_operation_1",
        status: "verified",
        result: "verified",
        apply_backup_id: "backup_rollback_apply",
        actions: [{ action_id: "migrate_power", status: "verified", trash_entry_id: "trash_rollback_power" }],
      }));
    vi.stubGlobal("fetch", fetchMock);
    const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await createPlan(user);
    await user.click(screen.getByRole("checkbox", { name: /migrate_power/ }));
    const applyButton = screen.getByRole("button", { name: "確認してrollback（Trashへ移動）" });
    expect(applyButton).toBeEnabled();

    await user.click(applyButton);
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(confirm.mock.calls[0][0]).toContain(eligiblePath);
    expect(confirm.mock.calls[0][0]).toContain("元のapply backup: backup_original_apply");
    expect(confirm.mock.calls[0][0]).toContain("Room / Device / IR scaffoldと旧sourceはrollback対象外");

    await user.click(applyButton);
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(await screen.findByText("backup_rollback_apply")).toBeInTheDocument();
    expect(screen.getByText("trash_rollback_power")).toBeInTheDocument();
    expect(screen.getByText(/Trash restoreまたはbackup restoreは別操作/)).toBeInTheDocument();
    const applyCall = fetchMock.mock.calls[1];
    expect(applyCall[0]).toBe("/api/controller/maintenance/migration/rollback/apply");
    expect(JSON.parse(String(applyCall[1]?.body))).toEqual({
      plan_id: planID,
      plan_digest: `sha256:${"e".repeat(64)}`,
      action_ids: ["migrate_power"],
      confirm: true,
    });
  });

  it("shows stale target or Trash conflict and requires a fresh dry-run rather than retrying the same apply", async () => {
    const replacementPlan = { ...rollbackDryRun(), plan: { ...rollbackDryRun().plan, plan_id: "rollback_plan_2" } };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(rollbackDryRun(), 201))
      .mockResolvedValueOnce(jsonResponse({ error: "rollback plan, target, or operation changed; inspect and create a new plan" }, 409))
      .mockResolvedValueOnce(jsonResponse(replacementPlan, 201));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await createPlan(user);
    await user.click(screen.getByRole("checkbox", { name: /migrate_power/ }));
    await user.click(screen.getByRole("button", { name: "確認してrollback（Trashへ移動）" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("rollback plan, target, or operation changed");
    expect(screen.getByRole("button", { name: "rollback要求済み（再実行不可）" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "新しいrollback dry-run" }));
    expect(await screen.findByText("rollback_plan_2")).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter((call) => call[0] === "/api/controller/maintenance/migration/rollback/apply")).toHaveLength(1);
  });

  it("preserves partial rollback operation and backup state without retrying", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(rollbackDryRun(), 201))
      .mockResolvedValueOnce(jsonResponse({
        ok: false,
        error: "migration rollback apply unavailable; inspect operation and maintenance state",
        operation_id: "rollback_partial_1",
        status: "recovery_required",
        apply_backup_id: "backup_partial_rollback",
      }, 503));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await createPlan(user);
    await user.click(screen.getByRole("checkbox", { name: /migrate_power/ }));
    await user.click(screen.getByRole("button", { name: "確認してrollback（Trashへ移動）" }));

    expect(await screen.findByText("rollback_partial_1")).toBeInTheDocument();
    expect(screen.getByText("backup_partial_rollback")).toBeInTheDocument();
    expect(screen.getByText(/rollbackを再試行せず/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "rollback要求済み（再実行不可）" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "再dry-run（新しい固定plan）" })).toBeDisabled();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("fails closed if the outer target list differs from the saved plan payload", async () => {
    const inconsistent = rollbackDryRun();
    inconsistent.targets = [modifiedTarget];
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(inconsistent, 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationRollbackSection operationID={operationID} />);
    await user.click(screen.getByRole("button", { name: "rollback dry-run" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("rollback dry-runのplan envelopeがoperationまたはtarget一覧と一致しません。");
    expect(screen.queryByRole("button", { name: "確認してrollback（Trashへ移動）" })).not.toBeInTheDocument();
  });
});
