import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { TrashDetailResponse, TrashListResponse } from "../../../api/controller";
import { TrashSection } from "./TrashSection";

function trashEntry(entryID = "trash_entry_1", targetType = "room") {
  return {
    entry_id: entryID,
    status: "sealed",
    target_type: targetType,
    original_id: "bedroom",
    original_relative_path: "rooms/bedroom/room.json",
    deleted_at: "2026-09-20T12:34:56Z",
    eligible_after: "2026-10-20T12:34:56Z",
    reason: "operator delete: bedroom room",
    operation_type: "delete",
    operation_id: "operation_delete_1",
    restore_available: true,
  };
}

function listResponse(entries = [trashEntry()], total = entries.length, offset = 0): TrashListResponse {
  return { entries, total, offset, limit: 25 };
}

function detailResponse(): TrashDetailResponse {
  return {
    ...trashEntry(),
    inventory: {
      kind: "file",
      file_count: 1,
      total_size: 128,
      sha256: `sha256:${"a".repeat(64)}`,
    },
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TrashSection", () => {
  it("shows loading, entry summaries, and read-only detail inventory", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(listResponse()))
      .mockResolvedValueOnce(jsonResponse(detailResponse()));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<TrashSection />);

    expect(screen.getByRole("status")).toHaveTextContent("読み込んでいます");
    expect(await screen.findByText("trash_entry_1")).toBeInTheDocument();
    expect(screen.getByText("rooms/bedroom/room.json")).toBeInTheDocument();
    expect(screen.getByText("2026-09-20T12:34:56Z (UTC)")).toBeInTheDocument();
    expect(screen.getByText(/operator delete: bedroom room/)).toBeInTheDocument();
    expect(screen.getByText(/operation_delete_1/)).toBeInTheDocument();
    expect(screen.getByText(/記録上 restore可能/)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/controller/maintenance/trash?offset=0&limit=25",
      { cache: "no-store" },
    );

    await user.click(screen.getByRole("button", { name: "詳細を表示: trash_entry_1" }));
    expect(await screen.findByText(/削除前inventory/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "詳細を再読み込み" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "このentryを復元" })).not.toBeInTheDocument();
    expect(screen.getByText(`sha256:${"a".repeat(64)}`)).toBeInTheDocument();
    expect(screen.getByText(/保存時の記録です/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /final-delete|最終削除/ })).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/controller/maintenance/trash/trash_entry_1",
      { cache: "no-store" },
    );
  });

  it("filters by target type and paginates using the filtered total", async () => {
    const firstPage = Array.from({ length: 25 }, (_, index) => trashEntry(`trash_${index}`, index === 0 ? "room" : "device"));
    const roomPage = Array.from({ length: 25 }, (_, index) => trashEntry(`room_${index}`, "room"));
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(listResponse(firstPage, 26)))
      .mockResolvedValueOnce(jsonResponse(listResponse(roomPage, 26)))
      .mockResolvedValueOnce(jsonResponse(listResponse([trashEntry("trash_last", "room")], 26, 25)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<TrashSection />);
    await screen.findByText("trash_0");

    await user.selectOptions(screen.getByRole("combobox", { name: "種別filter" }), "room");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/controller/maintenance/trash?type=room&offset=0&limit=25",
      { cache: "no-store" },
    );

    await user.click(screen.getByRole("button", { name: "次のページ" }));
    expect(await screen.findByText("trash_last")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/controller/maintenance/trash?type=room&offset=25&limit=25",
      { cache: "no-store" },
    );
    expect(screen.getByRole("button", { name: "前のページ" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "次のページ" })).toBeDisabled();
  });

  it("shows an empty state for an empty Trash", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(listResponse([], 0))));
    render(<TrashSection />);

    expect(await screen.findByText("Trash entryはありません。")).toBeInTheDocument();
  });

  it("surfaces unavailable list errors and supports retry", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: "Trash history unavailable" }, 503))
      .mockResolvedValueOnce(jsonResponse(listResponse()));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<TrashSection />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Trash history unavailable");
    await user.click(screen.getByRole("button", { name: "再試行" }));
    expect(await screen.findByText("trash_entry_1")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("keeps a list summary and explains when a selected corrupt/unavailable detail cannot be read", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(listResponse()))
      .mockResolvedValueOnce(jsonResponse({ error: "Trash history unavailable" }, 503));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<TrashSection />);
    await screen.findByText("trash_entry_1");
    await user.click(screen.getByRole("button", { name: "詳細を表示: trash_entry_1" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("破損metadataまたはBackend unavailableの可能性");
    expect(screen.getAllByText("rooms/bedroom/room.json").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText(/operator delete: bedroom room/).length).toBeGreaterThanOrEqual(2);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("updates the selected entry after the explicit restore flow succeeds", async () => {
    const preview = {
      entry_id: "trash_entry_1",
      original_id: "bedroom",
      original_relative_path: "rooms/bedroom/room.json",
      expected_inventory_hash: `sha256:${"b".repeat(64)}`,
      path_available: true,
      id_available: true,
      restore_available: true,
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(listResponse()))
      .mockResolvedValueOnce(jsonResponse(detailResponse()))
      .mockResolvedValueOnce(jsonResponse(preview))
      .mockResolvedValueOnce(jsonResponse({
        entry_id: "trash_entry_1",
        original_id: "bedroom",
        original_relative_path: "rooms/bedroom/room.json",
        operation_id: "restore_operation_1",
      }));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
    const user = userEvent.setup();

    render(<TrashSection />);
    await screen.findByText("trash_entry_1");
    await user.click(screen.getByRole("button", { name: "詳細を表示: trash_entry_1" }));
    await screen.findByText(/削除前inventory/);
    await user.click(screen.getByRole("button", { name: "復元可否を再確認（dry-run）" }));
    expect(await screen.findByText(/復元可能です/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "このentryを復元" }));

    expect(await screen.findByText(/restore_operation_1/)).toBeInTheDocument();
    expect(screen.getAllByText(/復元済み/).length).toBeGreaterThanOrEqual(2);
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/controller/maintenance/trash/trash_entry_1/restore",
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ expected_inventory_hash: preview.expected_inventory_hash, confirm: true }),
      }),
    );
  });
});
