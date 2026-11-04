import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TrashPage } from "./TrashPage";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("TrashPage", () => {
  it("loads and retries Trash independently from Maintenance APIs", async () => {
    let trashRequests = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.startsWith("/api/controller/maintenance/trash?")) {
        trashRequests += 1;
        if (trashRequests === 1) return jsonResponse({ error: "Trash fixture unavailable" }, 503);
        const query = new URLSearchParams(url.split("?")[1]);
        return jsonResponse({ entries: [], total: 0, offset: Number(query.get("offset")), limit: Number(query.get("limit")) });
      }
      return jsonResponse({ error: `unexpected Trash fixture request: ${url}` }, 500);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(
      <MemoryRouter initialEntries={["/trash"]}>
        <Routes>
          <Route path="/trash" element={<TrashPage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent("Trash fixture unavailable");
    expect(screen.getByRole("link", { name: "日常操作へ戻る" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Room / Device設定" })).toHaveAttribute("href", "/settings");
    expect(screen.getByRole("link", { name: "Maintenanceへ戻る" })).toHaveAttribute("href", "/maintenance");
    await user.click(screen.getByRole("button", { name: "再試行" }));
    expect(await screen.findByText("Trash entryはありません。")).toBeInTheDocument();
    expect(trashRequests).toBe(2);
    const requests = fetchMock.mock.calls.map(([url]) => String(url));
    expect(requests.every((url) => url.includes("/api/controller/maintenance/trash?"))).toBe(true);
  });
});
