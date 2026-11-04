import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HeaderTime } from "./HeaderTime";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("HeaderTime", () => {
  it("shows loading then server time without blocking surrounding content", async () => {
    let complete!: (response: Response) => void;
    vi.stubGlobal("fetch", vi.fn().mockReturnValue(new Promise<Response>((resolve) => { complete = resolve; })));
    render(<><p>Home操作</p><HeaderTime /></>);

    expect(screen.getByText("読み込み中...")).toHaveAttribute("role", "status");
    expect(screen.getByText("Home操作")).toBeInTheDocument();
    complete(new Response(JSON.stringify({ date: "2026-09-27", time: "12:34:56", datetime: "2026-09-27 12:34:56" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    expect(await screen.findByText("12:34:56")).toBeInTheDocument();
    expect(screen.queryByText("Time")).not.toBeInTheDocument();
    expect(screen.getByLabelText("現在時刻")).toContainElement(screen.getByText("12:34:56"));
  });

  it("shows a readable error while leaving the page usable", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    render(<><button type="button">Home操作</button><HeaderTime /></>);

    expect(await screen.findByText("時刻を取得できません")).toHaveAttribute("role", "status");
    expect(screen.getByRole("button", { name: "Home操作" })).toBeEnabled();
  });

  it("advances each second from server time without fetching every second", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-27T00:00:00Z"));
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      date: "2026-09-27", time: "12:34:56", datetime: "2026-09-27 12:34:56",
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    render(<HeaderTime />);
    await act(async () => { await Promise.resolve(); });

    expect(screen.getByText("12:34:56")).toBeInTheDocument();
    await act(async () => { vi.advanceTimersByTime(5_000); });
    expect(screen.getByText("12:35:01")).toBeInTheDocument();
    expect(screen.getByText("12:35:01").closest("time")).toHaveAttribute("datetime", "2026-09-27T12:35:01");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
