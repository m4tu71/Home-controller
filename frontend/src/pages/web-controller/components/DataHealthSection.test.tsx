import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DataHealthSection } from "./DataHealthSection";

const issue = {
  id: "invalid_json:rooms/bedroom/room.json",
  severity: "error" as const,
  path: "data/rooms/bedroom/room.json",
  hash: "a".repeat(64),
  message: "JSON file is invalid",
  has_destructive_actions: true,
};

const nonDestructiveIssue = {
  id: "unsafe_symlink:rooms/link",
  severity: "warning" as const,
  path: "data/rooms/link",
  hash: "b".repeat(64),
  message: "symbolic link is not followed",
  has_destructive_actions: false,
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DataHealthSection", () => {
  it("shows loading, issue details and starts a read-only rescan", async () => {
    let finishFirstScan: ((response: Response) => void) | undefined;
    const firstScan = new Promise<Response>((resolve) => {
      finishFirstScan = resolve;
    });
    const fetchMock = vi
      .fn()
      .mockReturnValueOnce(firstScan)
      .mockResolvedValue(
        jsonResponse({ ok: true, issues: [issue, nonDestructiveIssue] }),
      );
    vi.stubGlobal("fetch", fetchMock);

    render(<DataHealthSection />);

    expect(screen.getByRole("status")).toHaveTextContent("確認しています");
    finishFirstScan?.(
      jsonResponse({ ok: true, issues: [issue, nonDestructiveIssue] }),
    );

    expect(await screen.findByText(issue.message)).toBeInTheDocument();
    expect(screen.getAllByText(issue.path).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("エラー").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("警告").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText(/cleanup候補/).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("破壊的なcleanup操作なし")).toBeInTheDocument();
    expect(screen.getAllByText(issue.hash).length).toBeGreaterThanOrEqual(2);

    await userEvent.click(screen.getByRole("button", { name: "再スキャン" }));

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/controller/maintenance/data-health",
      { cache: "no-store" },
    );
  });

  it("shows the empty state when no issues are found", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse({ ok: true, issues: [] })),
    );

    render(<DataHealthSection />);

    expect(
      await screen.findByText("問題は検出されませんでした。"),
    ).toBeInTheDocument();
  });

  it("shows API failures and allows retrying the scan", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({ error: "data health scan failed" }, 503),
      )
      .mockResolvedValueOnce(jsonResponse({ ok: true, issues: [issue] }));
    vi.stubGlobal("fetch", fetchMock);

    render(<DataHealthSection />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "data health scan failed",
    );
    await userEvent.click(screen.getByRole("button", { name: "再試行" }));

    expect(await screen.findByText(issue.message)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
