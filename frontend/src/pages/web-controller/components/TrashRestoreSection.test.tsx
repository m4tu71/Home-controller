import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TrashRestoreSection } from "./TrashRestoreSection";

const entry = {
  entryID: "trash_entry_1",
  originalID: "bedroom",
  originalRelativePath: "rooms/bedroom/room.json",
  beforeInventorySHA256: `sha256:${"a".repeat(64)}`,
};

function preview(overrides: Record<string, unknown> = {}) {
  return {
    entry_id: entry.entryID,
    original_id: entry.originalID,
    original_relative_path: entry.originalRelativePath,
    expected_inventory_hash: `sha256:${"b".repeat(64)}`,
    path_available: true,
    id_available: true,
    restore_available: true,
    ...overrides,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderRestore(onRestored = vi.fn()) {
  render(<TrashRestoreSection {...entry} onRestored={onRestored} />);
  return onRestored;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TrashRestoreSection", () => {
  it("previews collisions, inventory changes and unavailable entries without offering an enabled restore", async () => {
    const cases = [
      { title: "path collision", response: preview({ path_available: false, restore_available: false, reason: "path_collision" }), text: "復元先pathが既に使用されています" },
      { title: "ID collision", response: preview({ id_available: false, restore_available: false, reason: "id_collision" }), text: "同一collection内に同じID" },
      { title: "changed payload", response: preview({ restore_available: false, reason: "inventory_changed" }), text: "削除時点からTrash payload" },
      { title: "unsealed entry", response: preview({ expected_inventory_hash: "", restore_available: false, reason: "entry_not_sealed" }), text: "復元可能な保持状態ではありません" },
    ];

    for (const testCase of cases) {
      const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(testCase.response));
      vi.stubGlobal("fetch", fetchMock);
      const user = userEvent.setup();
      const view = render(<TrashRestoreSection {...entry} onRestored={vi.fn()} />);

      await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
      expect(await screen.findByText(new RegExp(testCase.text))).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "このentryを復元" })).toBeDisabled();
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock).toHaveBeenCalledWith(
        `/api/controller/maintenance/trash/${entry.entryID}/restore/dry-run`,
        { method: "POST", cache: "no-store" },
      );
      view.unmount();
      vi.unstubAllGlobals();
    }
  });

  it("requires explicit confirmation and sends only the selected entry and preview hash", async () => {
    const expectedHash = `sha256:${"b".repeat(64)}`;
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(preview({ expected_inventory_hash: expectedHash })))
      .mockResolvedValueOnce(jsonResponse({
        entry_id: entry.entryID,
        original_id: entry.originalID,
        original_relative_path: entry.originalRelativePath,
        operation_id: "restore_operation_1",
      }));
    vi.stubGlobal("fetch", fetchMock);
    const confirmMock = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    vi.stubGlobal("confirm", confirmMock);
    const user = userEvent.setup();
    const onRestored = renderRestore();

    await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
    expect(await screen.findByText(/今回のdry-run確認hash/)).toBeInTheDocument();
    expect(screen.getByText(/削除前inventory SHA-256とは別の確認値/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "このentryを復元" }));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "このentryを復元" }));

    expect(await screen.findByText(/restore_operation_1/)).toBeInTheDocument();
    expect(confirmMock).toHaveBeenCalledTimes(2);
    expect(confirmMock.mock.calls[0][0]).toContain(entry.originalRelativePath);
    expect(confirmMock.mock.calls[0][0]).toContain(expectedHash);
    expect(confirmMock.mock.calls[0][0]).toContain("既存dataは上書きしません");
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      `/api/controller/maintenance/trash/${entry.entryID}/restore`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ expected_inventory_hash: expectedHash, confirm: true }),
        cache: "no-store",
      },
    );
    expect(onRestored).toHaveBeenCalledWith({
      entry_id: entry.entryID,
      original_id: entry.originalID,
      original_relative_path: entry.originalRelativePath,
      operation_id: "restore_operation_1",
    });
    expect(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" })).toBeDisabled();
  });

  it("blocks a duplicate restore while the first apply is in flight", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(preview()));
    let resolveApply: ((response: Response) => void) | undefined;
    const applyPromise = new Promise<Response>((resolve) => { resolveApply = resolve; });
    fetchMock.mockReturnValueOnce(applyPromise);
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();
    renderRestore();

    await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
    await screen.findByText(/復元可能です/);
    const restoreButton = screen.getByRole("button", { name: "このentryを復元" });
    await user.click(restoreButton);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(restoreButton).toBeDisabled();
    await user.click(restoreButton);
    expect(fetchMock).toHaveBeenCalledTimes(2);

    resolveApply?.(jsonResponse({
      entry_id: entry.entryID,
      original_id: entry.originalID,
      original_relative_path: entry.originalRelativePath,
      operation_id: "restore_operation_2",
    }));
    expect(await screen.findByText(/restore_operation_2/)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it.each([
    { status: 409, responseError: "Trash restore conflict", expectedMessage: "画面を更新して再確認してください", summary: "競合" },
    { status: 503, responseError: "Trash restore unavailable", expectedMessage: "部分的に進んだ可能性", summary: "部分失敗" },
  ])("fails closed on $summary and blocks retry", async ({ status, responseError, expectedMessage }) => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(preview()))
      .mockResolvedValueOnce(jsonResponse({ error: responseError }, status));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();
    renderRestore();

    await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
    await screen.findByText(/復元可能です/);
    await user.click(screen.getByRole("button", { name: "このentryを復元" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(expectedMessage);
    expect(screen.getByText(/同じentryの再実行はブロックされています/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "このentryを復元" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "このentryを復元" }));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("rejects a dry-run response for a different entry before offering restore", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(preview({ original_relative_path: "rooms/other/room.json" })));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    renderRestore();

    await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("選択中のTrash entryと一致しません");
    expect(screen.queryByRole("button", { name: "このentryを復元" })).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
