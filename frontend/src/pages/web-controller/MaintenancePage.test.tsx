import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MaintenancePage } from "./MaintenancePage";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("MaintenancePage", () => {
  it("mounts data health, migration, and operation history only on its route", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/controller/maintenance/data-health") {
        return jsonResponse({ error: "maintenance fixture unavailable" }, 503);
      }
      if (url === "/api/controller/maintenance/operations") return jsonResponse({ operations: [] });
      return jsonResponse({ error: `unexpected maintenance fixture request: ${url}` }, 500);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <MemoryRouter initialEntries={["/maintenance"]}>
        <Routes>
          <Route path="/maintenance" element={<MaintenancePage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent("maintenance fixture unavailable");
    expect(await screen.findByText("Maintenance operation履歴はありません。")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Migration plan / dry-run" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "日常操作へ戻る" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Room / Device設定" })).toHaveAttribute("href", "/settings");
    expect(screen.getByRole("link", { name: "Trashを確認" })).toHaveAttribute("href", "/trash");
    const requests = fetchMock.mock.calls.map(([url]) => String(url));
    expect(requests).toContain("/api/controller/maintenance/data-health");
    expect(requests).toContain("/api/controller/maintenance/operations");
    expect(requests.some((url) => url.includes("/maintenance/trash"))).toBe(false);
  });
});
