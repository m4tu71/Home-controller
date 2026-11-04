import { render as renderRTL, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { OperationHistorySection } from "./OperationHistorySection";

function render(ui: ReactElement) {
  return renderRTL(ui, { wrapper: MemoryRouter });
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const migrationSummary = {
  operation_id: "op_migration_1",
  plan_id: "plan_1",
  type: "migration",
  target: "migration",
  status: "recovery_required",
  started_at: "2026-09-25T01:02:03Z",
  completed_at: null,
  rehearsal_backup_id: "backup_rehearsal_1",
  apply_backup_id: "backup_apply_1",
};

const recoveryDetail = {
  ...migrationSummary,
  previous_status: "applying",
  actions: [{
    action_id: "migrate_power",
    action: "write_ir_command",
    status: "applied_unconfirmed",
    classification: "applied_unconfirmed",
    reason: "runtime_read_failed",
  }],
  error: "recorded failure; inspect backend host for details",
  available_next_actions: ["verify", "restore", "new_dry_run"],
};

const rehearsalBackup = {
  backup_id: "backup_rehearsal_1",
  role: "rehearsal",
  state: "rehearsal_verified",
  plan_id: "plan_1",
  action_ids: ["migrate_power"],
  file_count: 2,
  total_size: 128,
  inventory_hash: "a".repeat(64),
  created_at: "2026-09-25T01:00:00Z",
  sources: ["rooms"],
  files: [],
};

const applyBackup = {
  ...rehearsalBackup,
  backup_id: "backup_apply_1",
  role: "apply",
  state: "point_in_time_verified",
  inventory_hash: "b".repeat(64),
};

function historyFetch(overrides: Record<string, Response> = {}) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const override = overrides[url];
    if (override) return override;
    if (url === "/api/controller/maintenance/operations") {
      return jsonResponse({ operations: [migrationSummary] });
    }
    if (url === "/api/controller/maintenance/operations/op_migration_1") {
      return jsonResponse(recoveryDetail);
    }
    if (url === "/api/controller/maintenance/backups/backup_rehearsal_1") {
      return jsonResponse(rehearsalBackup);
    }
    if (url === "/api/controller/maintenance/backups/backup_apply_1") {
      return jsonResponse(applyBackup);
    }
    if (url === "/api/controller/maintenance/migration/verify") {
      expect(init?.method).toBe("POST");
      expect(init?.body).toBe(JSON.stringify({ operation_id: "op_migration_1" }));
      return jsonResponse({
        operation_id: "op_migration_1",
        plan_id: "plan_1",
        status: "verified",
        targets: [{ action_id: "migrate_power", target_path: "rooms/bedroom/devices/light", status: "verified", expected_sha256: "c".repeat(64), actual_sha256: "c".repeat(64) }],
      });
    }
    return jsonResponse({ error: `unexpected request: ${url}` }, 500);
  });
}

async function alertContaining(text: string): Promise<HTMLElement> {
  const alerts = await screen.findAllByRole("alert");
  const match = alerts.find((alert) => alert.textContent?.includes(text));
  if (!match) throw new Error(`alert did not contain: ${text}`);
  return match;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("OperationHistorySection", () => {
  it("lists safe operation summaries, loads action and backup details, and runs only read-only migration verify", async () => {
    const fetchMock = historyFetch();
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);

    expect(screen.getByRole("status")).toHaveTextContent("読み込んでいます");
    expect(await screen.findByText("要手動確認")).toBeInTheDocument();
    expect(screen.getByText("対象識別子").parentElement).toHaveTextContent("migration");
    expect(screen.getByText("開始 / 完了 (UTC)").parentElement).toHaveTextContent("記録なし（未完了の可能性）");

    await userEvent.click(screen.getByRole("button", { name: "詳細を表示: op_migration_1" }));

    expect(await screen.findByText("write_ir_command / 適用済み・未確認")).toBeInTheDocument();
    expect(screen.getByText(/apply backupのrole\/stateを上で確認/)).toBeInTheDocument();
    expect(await screen.findByText(/Role: rehearsal; state: rehearsal_verified/)).toBeInTheDocument();
    expect(await screen.findByText(/Role: apply; state: point_in_time_verified/)).toBeInTheDocument();
    expect(screen.getByText(/read-only verify候補/)).toBeInTheDocument();
    expect(screen.queryByText(/before_state|intended_state|after_state/)).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Migrationをread-only verify" }));

    expect(await screen.findByText("Repository verify: 検証済み")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/controller/maintenance/migration/verify",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows empty history and retries after the history API is unavailable", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: "maintenance history unavailable" }, 503))
      .mockResolvedValueOnce(jsonResponse({ operations: [] }));
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);

    expect(await screen.findByRole("alert")).toHaveTextContent("maintenance history unavailable");
    await userEvent.click(screen.getByRole("button", { name: "再試行" }));
    expect(await screen.findByText("Maintenance operation履歴はありません。")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("keeps operation context visible when an associated backup is missing", async () => {
    const fetchMock = historyFetch({
      "/api/controller/maintenance/backups/backup_rehearsal_1": jsonResponse({ error: "backup not found" }, 404),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);
    await userEvent.click(await screen.findByRole("button", { name: "詳細を表示: op_migration_1" }));

    expect(await alertContaining("backup not found")).toHaveTextContent("復元可能とは判断しないでください");
    expect(screen.getByText("migrate_power")).toBeInTheDocument();
    expect(screen.getByText(/復元可能とは判断しないでください/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Migrationをread-only verify" })).toBeInTheDocument();
  });

  it("does not mark backup details as valid when role, state, plan or actions disagree", async () => {
    const wrongBackup = {
      ...rehearsalBackup,
      role: "apply",
      state: "snapshot_verified",
      plan_id: "other_plan",
      action_ids: ["other_action"],
    };
    vi.stubGlobal("fetch", historyFetch({
      "/api/controller/maintenance/backups/backup_rehearsal_1": jsonResponse(wrongBackup),
    }));

    render(<OperationHistorySection />);
    await userEvent.click(await screen.findByRole("button", { name: "詳細を表示: op_migration_1" }));

    expect(await alertContaining("roleがrehearsalではありません")).toHaveTextContent("action ID一覧がoperationと一致しません");
    expect(screen.getAllByText(/role \/ state \/ plan \/ action IDをoperationと照合しました/)).toHaveLength(1);
  });

  it("shows detail API errors and keeps the selected summary available", async () => {
    vi.stubGlobal("fetch", historyFetch({
      "/api/controller/maintenance/operations/op_migration_1": jsonResponse({ error: "operation not found" }, 404),
    }));

    render(<OperationHistorySection />);
    await userEvent.click(await screen.findByRole("button", { name: "詳細を表示: op_migration_1" }));

    expect(await alertContaining("operation not found")).toBeInTheDocument();
    expect(screen.getByText("op_migration_1")).toBeInTheDocument();
  });

  it("labels failure and recovery statuses across operation records", async () => {
    const statuses = [
      ["prepared", "準備済み"],
      ["applying", "適用中"],
      ["applied", "適用済み・未検証"],
      ["verifying", "検証中"],
      ["verified", "検証済み"],
      ["partial_failure", "部分失敗"],
      ["verification_failed", "検証失敗"],
      ["recovery_required", "要手動確認"],
    ];
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({
      operations: statuses.map(([status], index) => ({
        ...migrationSummary,
        operation_id: `op_${index}`,
        status,
        rehearsal_backup_id: "",
        apply_backup_id: "",
      })),
    }));
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);

    for (const [, text] of statuses) expect(await screen.findAllByText(text)).not.toHaveLength(0);
  });

  it("offers manual inspection only for ambiguous recovery and does not expose verify", async () => {
    const ambiguousDetail = {
      ...recoveryDetail,
      actions: [{
        action_id: "cleanup_one",
        action: "trash_cleanup",
        status: "applying",
        classification: "ambiguous",
        reason: "filesystem_mismatch",
      }],
      available_next_actions: ["manual_inspection"],
    };
    const fetchMock = historyFetch({
      "/api/controller/maintenance/operations": jsonResponse({ operations: [{ ...migrationSummary, type: "cleanup" }] }),
      "/api/controller/maintenance/operations/op_migration_1": jsonResponse({ ...ambiguousDetail, type: "cleanup" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);
    await userEvent.click(await screen.findByRole("button", { name: "詳細を表示: op_migration_1" }));

    expect(await alertContaining("状態が曖昧です。")).toHaveTextContent("Backend host上でruntime data");
    expect(screen.queryByRole("button", { name: "Migrationをread-only verify" })).not.toBeInTheDocument();
    expect(screen.getByText("cleanup_one")).toBeInTheDocument();
  });

  it("fails closed when verify returns a different operation ID", async () => {
    const fetchMock = historyFetch({
      "/api/controller/maintenance/migration/verify": jsonResponse({
        operation_id: "other_operation",
        plan_id: "plan_1",
        status: "verified",
        targets: [],
      }),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<OperationHistorySection />);
    await userEvent.click(await screen.findByRole("button", { name: "詳細を表示: op_migration_1" }));
    await userEvent.click(await screen.findByRole("button", { name: "Migrationをread-only verify" }));

    expect(await alertContaining("verify結果のoperation/plan IDが選択中の履歴と一致しません。")).toBeInTheDocument();
    expect(screen.queryByText(/Repository verify:/)).not.toBeInTheDocument();
  });
});
