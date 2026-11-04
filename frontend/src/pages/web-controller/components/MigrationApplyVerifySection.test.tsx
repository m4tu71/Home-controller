import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { MigrationDryRunPlan } from "../../../api/controller";
import { MigrationApplyVerifySection } from "./MigrationApplyVerifySection";

const actionPath = "rooms/living/devices/tv/adapters/ir/commands/power.json";
const bedroomV2ActionIDs = [
  "roomlight_on_ch3",
  "roomlight_full_ch3",
  "roomlight_off_ch3",
  "roomlight_night_ch3",
  "roomlight_bright_up_ch3",
  "roomlight_bright_down_ch3",
];
const plan: MigrationDryRunPlan = {
  version: 1,
  plan_id: "plan_migration_8b",
  operation_type: "migration",
  payload_schema_id: "migration.v1",
  payload: {
    actions: [{
      action_id: "migrate_power",
      action: "write_ir_command",
      source: { path: "ir-signals/power.json", exists: true, kind: "file", size: 64, sha256: "source-hash" },
      source_format: "raw",
      target_before: { path: actionPath, exists: false, size: 0 },
      planned_writes: [{
        path: actionPath,
        kind: "json",
        role: "operation",
        before: { path: actionPath, exists: false, size: 0 },
        expected_after_state: "created",
      }],
      target_format: "raw",
      transform: "preserve_raw",
      expected_after_state: "target_created",
    }],
  },
  created_at: "2026-09-25T00:00:00Z",
  ready_to_apply: true,
  backup_requirement: "rehearsal_and_apply",
  actions: [{
    action_id: "migrate_power",
    action: "write_ir_command",
    selectable: true,
    destructive: false,
    backup_sources: ["rooms"],
    backup_inventory_hash: `sha256:${"a".repeat(64)}`,
    backup_file_count: 1,
    backup_total_size: 64,
  }],
  plan_digest: `sha256:${"b".repeat(64)}`,
};

const backupSummary = {
  backup_id: "backup_rehearsal_8b",
  role: "rehearsal",
  state: "snapshot_verified",
  plan_id: plan.plan_id,
  action_ids: ["migrate_power"],
  file_count: 1,
  total_size: 64,
  inventory_hash: `sha256:${"c".repeat(64)}`,
};

function detail(state: string) {
  return {
    ...backupSummary,
    state,
    created_at: "2026-09-25T00:01:00Z",
    sources: ["rooms"],
    files: [{ path: "rooms/living/room.json", size: 64, sha256: "room-hash", json_status: "valid" }],
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function installSuccessfulBackupPipeline(fetchMock = vi.fn(), canRehearse = true) {
  fetchMock
    .mockResolvedValueOnce(jsonResponse(backupSummary, 201))
    .mockResolvedValueOnce(jsonResponse(detail("snapshot_verified")))
    .mockResolvedValueOnce(jsonResponse({
      backup_id: backupSummary.backup_id,
      role: "rehearsal",
      state: "snapshot_verified",
      target: "isolated-rehearsal/backup_rehearsal_8b",
      required_bytes: 4096,
      available_bytes: 8192,
      source_inventory_hash: backupSummary.inventory_hash,
      target_exists: false,
      can_rehearse: canRehearse,
      ...(canRehearse ? {} : { reason: "target_collision" }),
    }))
    .mockResolvedValueOnce(jsonResponse({
      backup_id: backupSummary.backup_id,
      role: "rehearsal",
      previous_state: "snapshot_verified",
      state: "rehearsal_verified",
      target: "isolated-rehearsal/backup_rehearsal_8b",
      inventory_hash: backupSummary.inventory_hash,
      file_count: 1,
      total_size: 64,
    }))
    .mockResolvedValueOnce(jsonResponse(detail("rehearsal_verified")));
  return fetchMock;
}

function bedroomV2Plan(): MigrationDryRunPlan {
  const payloadActions = bedroomV2ActionIDs.map((actionID) => {
    const targetPath = `rooms/bedroom/devices/roomlight/adapters/ir/commands/${actionID}.json`;
    return {
      ...plan.payload.actions[0],
      action_id: actionID,
      target_before: { path: targetPath, exists: false, size: 0 },
      planned_writes: [{
        ...plan.payload.actions[0].planned_writes[0],
        path: targetPath,
        before: { path: targetPath, exists: false, size: 0 },
      }],
    };
  });
  return {
    ...plan,
    plan_id: "plan_bedroom_v2",
    payload_schema_id: "migration.v2",
    payload: { actions: payloadActions },
    actions: bedroomV2ActionIDs.map((actionID) => ({
      ...plan.actions[0],
      action_id: actionID,
      backup_sources: ["rooms"],
    })),
  };
}

async function selectAction(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("checkbox", { name: /migrate_power/ }));
}

async function reachApplyGate(user: ReturnType<typeof userEvent.setup>, fetchMock: ReturnType<typeof installSuccessfulBackupPipeline>) {
  await selectAction(user);
  await user.click(screen.getByRole("button", { name: "選択actionのrehearsal backupを作成" }));
  await screen.findByText("snapshot_verified");
  await user.click(screen.getByRole("button", { name: "隔離rehearsalのdry-run" }));
  await screen.findByText("isolated-rehearsal/backup_rehearsal_8b");
  await user.click(screen.getByRole("button", { name: "隔離rehearsalを実行" }));
  await screen.findByText("repository/byte readback済み。実runtime dataへのrestoreではありません。");
  expect(fetchMock).toHaveBeenCalledTimes(5);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MigrationApplyVerifySection", () => {
  it("does not create a backup until at least one selectable action is explicitly selected", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    render(<MigrationApplyVerifySection plan={plan} />);

    expect(screen.getByRole("button", { name: "選択actionのrehearsal backupを作成" })).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
    await selectAction(userEvent.setup());
    expect(screen.getByRole("button", { name: "選択actionのrehearsal backupを作成" })).toBeEnabled();
  });

  it("requires all six fixed migration.v2 actions before creating its rehearsal backup", async () => {
    const v2Plan = bedroomV2Plan();
    const v2Backup = { ...backupSummary, plan_id: v2Plan.plan_id, action_ids: bedroomV2ActionIDs };
    const v2Detail = {
      ...detail("snapshot_verified"),
      plan_id: v2Plan.plan_id,
      action_ids: bedroomV2ActionIDs,
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(v2Backup, 201))
      .mockResolvedValueOnce(jsonResponse(v2Detail));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationApplyVerifySection plan={v2Plan} />);
    const backupButtonName = "6件すべて選択してrehearsal backupを作成";
    const backupButton = screen.getByRole("button", { name: backupButtonName });
    expect(backupButton).toBeDisabled();

    for (const actionID of bedroomV2ActionIDs.slice(0, 5)) {
      await user.click(screen.getByRole("checkbox", { name: new RegExp(actionID) }));
    }
    expect(screen.getByText("migration.v2は6 action全件が必須です（現在 5/6 件選択）。")).toBeInTheDocument();
    expect(screen.getByText("6件すべてを選択するまでrehearsal backup・rehearsal・applyへ進めません。")).toBeInTheDocument();
    expect(backupButton).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();

    await user.click(screen.getByRole("checkbox", { name: /roomlight_bright_down_ch3/ }));
    expect(screen.getByText("migration.v2は6 action全件が必須です（現在 6/6 件選択）。")).toBeInTheDocument();
    expect(backupButton).toBeEnabled();
    await user.click(backupButton);
    expect(await screen.findByText("snapshot_verified")).toBeInTheDocument();
    const createCall = fetchMock.mock.calls[0];
    expect(createCall[0]).toBe("/api/controller/maintenance/backups");
    expect(JSON.parse(String(createCall[1]?.body))).toEqual({
      plan_id: v2Plan.plan_id,
      plan_digest: v2Plan.plan_digest,
      action_ids: bedroomV2ActionIDs,
    });
  });

  it("fails closed when a migration.v2 plan does not contain the exact six approved action IDs", () => {
    const invalidV2Plan = bedroomV2Plan();
    invalidV2Plan.actions = invalidV2Plan.actions.slice(1);
    invalidV2Plan.payload.actions = invalidV2Plan.payload.actions.slice(1);
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    render(<MigrationApplyVerifySection plan={invalidV2Plan} />);

    expect(screen.getByRole("alert")).toHaveTextContent("規定の6件と一致しないため、backup / applyを停止しています。");
    expect(screen.getByRole("button", { name: "6件すべて選択してrehearsal backupを作成" })).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("surfaces backup creation failure and keeps apply gated", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse({ error: "plan or backup source changed; create a new plan" }, 409));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationApplyVerifySection plan={plan} />);
    await selectAction(user);
    await user.click(screen.getByRole("button", { name: "選択actionのrehearsal backupを作成" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("plan or backup source changed");
    expect(screen.queryByRole("button", { name: "確認してapply" })).not.toBeInTheDocument();
  });

  it("keeps apply disabled when isolated rehearsal preview reports a collision", async () => {
    const fetchMock = installSuccessfulBackupPipeline(vi.fn(), false);
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<MigrationApplyVerifySection plan={plan} />);
    await selectAction(user);
    await user.click(screen.getByRole("button", { name: "選択actionのrehearsal backupを作成" }));
    await screen.findByText("snapshot_verified");
    await user.click(screen.getByRole("button", { name: "隔離rehearsalのdry-run" }));

    expect(await screen.findByText("不可: target_collision")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "隔離rehearsalを実行" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "確認してapply" })).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("requires native confirmation, applies the exact selection, and shows repository verification differences", async () => {
    const fetchMock = installSuccessfulBackupPipeline();
    fetchMock
      .mockResolvedValueOnce(jsonResponse({
        ok: true,
        operation_id: "migration_op_8b",
        status: "applied_unconfirmed",
        result: "partial",
        rehearsal_backup_id: backupSummary.backup_id,
        apply_backup_id: "backup_apply_8b",
        actions: [{ id: "migrate_power", action: "write_ir_command", status: "applied_unconfirmed" }],
      }))
      .mockResolvedValueOnce(jsonResponse({
        operation_id: "migration_op_8b",
        plan_id: plan.plan_id,
        status: "failed",
        targets: [{
          action_id: "migrate_power",
          target_path: actionPath,
          status: "failed",
          reason: "target hash differs",
          expected_sha256: "expected-hash",
          actual_sha256: "actual-hash",
        }],
      }));
    vi.stubGlobal("fetch", fetchMock);
    const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();

    render(<MigrationApplyVerifySection plan={plan} />);
    await reachApplyGate(user, fetchMock);
    const applyButton = screen.getByRole("button", { name: "確認してapply" });
    await user.click(applyButton);
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(5);

    await user.click(applyButton);
    expect(confirm).toHaveBeenCalledTimes(2);
    await screen.findByText("backup_apply_8b");
    const applyCall = fetchMock.mock.calls.find((call) => call[0] === "/api/controller/maintenance/migration/apply");
    expect(applyCall?.[1]).toMatchObject({ method: "POST", cache: "no-store" });
    expect(JSON.parse(String(applyCall?.[1]?.body))).toEqual({
      plan_id: plan.plan_id,
      plan_digest: plan.plan_digest,
      action_ids: ["migrate_power"],
      rehearsal_backup_id: backupSummary.backup_id,
      confirm: true,
    });

    await user.click(screen.getByRole("button", { name: "operationをverify（read-only）" }));
    expect(await screen.findByText("Repository verify: failed")).toBeInTheDocument();
    expect(screen.getByText(/target hash differs/)).toBeInTheDocument();
    expect(screen.getByText("expected-hash")).toBeInTheDocument();
    expect(screen.getByText("actual-hash")).toBeInTheDocument();
    const verifyCall = fetchMock.mock.calls.find((call) => call[0] === "/api/controller/maintenance/migration/verify");
    expect(JSON.parse(String(verifyCall?.[1]?.body))).toEqual({ operation_id: "migration_op_8b" });
  });

  it("preserves partial apply identifiers after a stale/error response and surfaces verify failure without retrying apply", async () => {
    const fetchMock = installSuccessfulBackupPipeline();
    fetchMock
      .mockResolvedValueOnce(jsonResponse({
        ok: false,
        error: "migration source, target, backup, or operation changed",
        operation_id: "migration_partial_8b",
        status: "recovery_required",
        apply_backup_id: "backup_partial_8b",
      }, 409))
      .mockResolvedValueOnce(jsonResponse({ error: "migration operation unavailable" }, 503));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();

    render(<MigrationApplyVerifySection plan={plan} />);
    await reachApplyGate(user, fetchMock);
    await user.click(screen.getByRole("button", { name: "確認してapply" }));

    expect((await screen.findAllByText("migration_partial_8b")).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("backup_partial_8b")).toBeInTheDocument();
    expect(screen.getByText(/applyを再試行しないでください/)).toBeInTheDocument();
    const applyButton = screen.getByRole("button", { name: "apply要求済み（再実行不可）" });
    expect(applyButton).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "operationをverify（read-only）" }));
    await waitFor(() => expect(
      screen.getAllByRole("alert").some((alert) => alert.textContent?.includes("verifyに失敗しました: migration operation unavailable")),
    ).toBe(true));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(7));
    expect(fetchMock.mock.calls.filter((call) => call[0] === "/api/controller/maintenance/migration/apply")).toHaveLength(1);
  });
});
