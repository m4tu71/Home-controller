import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CleanupDryRunResponse, DataHealthIssue } from "../../../api/controller";
import { DataCleanupSection } from "./DataCleanupSection";

const cleanupIssue: DataHealthIssue = {
  id: "invalid_json:rooms/bedroom/room.json",
  severity: "error",
  path: "data/rooms/bedroom/room.json",
  hash: "a".repeat(64),
  message: "JSON file is invalid",
  has_destructive_actions: true,
};

const nonDestructiveIssue: DataHealthIssue = {
  id: "unsafe_symlink:rooms/link",
  severity: "warning",
  path: "data/rooms/link",
  hash: "b".repeat(64),
  message: "symbolic link is not followed",
  has_destructive_actions: false,
};

const action = {
  issue_id: cleanupIssue.id,
  action_id: "cleanup_invalid_room_json",
  action: "trash_file" as const,
  destructive: true,
  affected_paths: [cleanupIssue.path],
  before_hash: cleanupIssue.hash,
  expected_after_state: "moved_to_trash" as const,
  backup_sources: [cleanupIssue.path],
  backup_inventory_hash: `sha256:${"c".repeat(64)}`,
  backup_file_count: 1,
  backup_total_size: 84,
};

function dryRunResponse(planID = "cleanup_plan_1"): CleanupDryRunResponse {
  return {
    ok: true,
    plan_id: planID,
    plan_digest: `sha256:${"d".repeat(64)}`,
    ready_to_apply: true,
    backup_requirement: "apply",
    backup_planned: true,
    actions: [action],
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

type User = ReturnType<typeof userEvent.setup>;

async function createPlan(user: User) {
  await user.click(screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ }));
  await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));
  await screen.findByText("cleanup_plan_1");
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DataCleanupSection", () => {
  it("starts with no issue/action selected and reviews affected paths, hashes and planned backup", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(dryRunResponse(), 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(
      <DataCleanupSection
        issues={[cleanupIssue, nonDestructiveIssue]}
        scanRevision={1}
        scanBusy={false}
        scanError=""
        onRescan={vi.fn()}
      />,
    );

    const issueCheckbox = screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ });
    expect(issueCheckbox).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /unsafe_symlink:rooms\/link/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "選択issueでcleanup dry-run" })).toBeDisabled();

    await user.click(issueCheckbox);
    await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));

    expect(await screen.findByText("cleanup_plan_1")).toBeInTheDocument();
    expect(screen.getAllByText(cleanupIssue.path).length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText(cleanupIssue.hash).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("moved_to_trash")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /cleanup_invalid_room_json/ })).toBeInTheDocument();
    expect(screen.getByText(/sha256:c{64}/)).toBeInTheDocument();
    expect(screen.getByText(/まだ作成されていません/)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /cleanup_invalid_room_json/ })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "確認してcleanup apply（Trashへ移動）" })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/controller/maintenance/data-cleanup/dry-run",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ issues: [{ issue_id: cleanupIssue.id, before_hash: cleanupIssue.hash }] }),
        cache: "no-store",
      }),
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("requires action selection and confirmation, then shows backup and Trash results and allows a rescan", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(dryRunResponse(), 201))
      .mockResolvedValueOnce(jsonResponse({
        ok: true,
        operation_id: "cleanup_operation_1",
        status: "verified",
        result: "verified",
        apply_backup_id: "backup_cleanup_1",
        actions: [{ action_id: action.action_id, status: "verified", trash_entry_id: "trash_cleanup_1" }],
      }));
    vi.stubGlobal("fetch", fetchMock);
    const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    vi.stubGlobal("confirm", confirm);
    const onRescan = vi.fn();
    const user = userEvent.setup();

    render(
      <DataCleanupSection
        issues={[cleanupIssue]}
        scanRevision={4}
        scanBusy={false}
        scanError=""
        onRescan={onRescan}
      />,
    );

    await createPlan(user);
    const actionCheckbox = screen.getByRole("checkbox", { name: /cleanup_invalid_room_json/ });
    await user.click(actionCheckbox);
    const applyButton = screen.getByRole("button", { name: "確認してcleanup apply（Trashへ移動）" });
    expect(applyButton).toBeEnabled();

    await user.click(applyButton);
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(confirm.mock.calls[0][0]).toContain(cleanupIssue.path);
    expect(confirm.mock.calls[0][0]).toContain("point-in-time backup");
    expect(confirm.mock.calls[0][0]).toContain("物理的な最終削除やbackup restoreは行いません");

    await user.click(applyButton);
    expect(await screen.findByText("cleanup_operation_1")).toBeInTheDocument();
    expect(screen.getByText("backup_cleanup_1")).toBeInTheDocument();
    expect(screen.getByText("trash_cleanup_1")).toBeInTheDocument();
    expect(screen.getByText(/最終削除はこのUIでは実行できません/)).toBeInTheDocument();
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      plan_id: "cleanup_plan_1",
      plan_digest: `sha256:${"d".repeat(64)}`,
      issues: [{ issue_id: cleanupIssue.id, before_hash: cleanupIssue.hash }],
      action_ids: [action.action_id],
      confirm: true,
    });

    await user.click(screen.getByRole("button", { name: "Data Healthを再スキャン" }));
    expect(onRescan).toHaveBeenCalledTimes(1);
  });

  it("fails closed on a stale dry-run response and requires a rescan plus fresh issue selection", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: "cleanup target changed; scan again" }, 409))
      .mockResolvedValueOnce(jsonResponse(dryRunResponse("cleanup_plan_2"), 201));
    vi.stubGlobal("fetch", fetchMock);
    const onRescan = vi.fn();
    const user = userEvent.setup();
    const props = {
      issues: [cleanupIssue],
      scanRevision: 7,
      scanBusy: false,
      scanError: "",
      onRescan,
    };
    const view = render(<DataCleanupSection {...props} />);

    await user.click(screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ }));
    await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("cleanup target changed; scan again");
    expect(screen.getByRole("button", { name: "選択issueでcleanup dry-run" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Data Healthを再スキャン" }));
    expect(onRescan).toHaveBeenCalledTimes(1);

    view.rerender(<DataCleanupSection {...props} scanRevision={8} />);
    const refreshedIssue = screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ });
    expect(refreshedIssue).not.toBeChecked();
    await user.click(refreshedIssue);
    await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));
    expect(await screen.findByText("cleanup_plan_2")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not retry a stale apply with the same plan and invalidates it after rescanning", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(dryRunResponse(), 201))
      .mockResolvedValueOnce(jsonResponse({ error: "cleanup plan, target, or operation changed; inspect and retry with a new plan" }, 409))
      .mockResolvedValueOnce(jsonResponse(dryRunResponse("cleanup_plan_2"), 201));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const onRescan = vi.fn();
    const user = userEvent.setup();
    const props = {
      issues: [cleanupIssue],
      scanRevision: 10,
      scanBusy: false,
      scanError: "",
      onRescan,
    };
    const view = render(<DataCleanupSection {...props} />);

    await createPlan(user);
    await user.click(screen.getByRole("checkbox", { name: /cleanup_invalid_room_json/ }));
    await user.click(screen.getByRole("button", { name: "確認してcleanup apply（Trashへ移動）" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("cleanup plan, target, or operation changed");
    expect(screen.getByRole("button", { name: "apply要求済み（再実行不可）" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Data Healthを再スキャン" }));
    expect(onRescan).toHaveBeenCalledTimes(1);

    view.rerender(<DataCleanupSection {...props} scanRevision={11} />);
    expect(screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ })).not.toBeChecked();
    await user.click(screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ }));
    await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));
    expect(await screen.findByText("cleanup_plan_2")).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter((call) => call[0] === "/api/controller/maintenance/data-cleanup/apply")).toHaveLength(1);
  });

  it("preserves operation and backup identifiers on partial failure and blocks another apply", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(dryRunResponse(), 201))
      .mockResolvedValueOnce(jsonResponse({
        ok: false,
        error: "cleanup apply unavailable; inspect operation and maintenance state",
        operation_id: "cleanup_partial_1",
        status: "recovery_required",
        apply_backup_id: "backup_partial_cleanup",
      }, 503));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();

    render(
      <DataCleanupSection
        issues={[cleanupIssue]}
        scanRevision={15}
        scanBusy={false}
        scanError=""
        onRescan={vi.fn()}
      />,
    );

    await createPlan(user);
    await user.click(screen.getByRole("checkbox", { name: /cleanup_invalid_room_json/ }));
    await user.click(screen.getByRole("button", { name: "確認してcleanup apply（Trashへ移動）" }));

    expect(await screen.findByText("cleanup_partial_1")).toBeInTheDocument();
    expect(screen.getByText("backup_partial_cleanup")).toBeInTheDocument();
    expect(screen.getByText(/cleanup applyを再試行せず/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "apply要求済み（再実行不可）" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "選択issueでcleanup dry-run" })).toBeDisabled();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("fails closed when dry-run actions do not match selected issues", async () => {
    const invalidResponse = dryRunResponse();
    invalidResponse.actions = [{ ...action, issue_id: "unsupported:rooms/other" }];
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(invalidResponse, 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(
      <DataCleanupSection
        issues={[cleanupIssue]}
        scanRevision={20}
        scanBusy={false}
        scanError=""
        onRescan={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("checkbox", { name: /invalid_json:rooms\/bedroom\/room.json/ }));
    await user.click(screen.getByRole("button", { name: "選択issueでcleanup dry-run" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("cleanup dry-run actionが選択issueまたはbackup inventoryと一致しません");
    expect(screen.queryByRole("button", { name: "確認してcleanup apply（Trashへ移動）" })).not.toBeInTheDocument();
  });
});
