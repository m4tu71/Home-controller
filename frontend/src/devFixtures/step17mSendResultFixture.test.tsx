import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import WebController from "../pages/web-controller/WebController";
import { installStep17MSendResultFixture } from "./step17mSendResultFixture";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Step 17M send-result browser fixture", () => {
  it.each([
    { delivery: "sent", expected: "送信成功" },
    { delivery: "not_sent", expected: "送信は開始されていません" },
    { delivery: "partially_sent", expected: "1回中0回の成功を確認しました" },
    { delivery: "unknown", expected: "送信済みの可能性があります" },
  ] as const)("shows $delivery without forwarding controller requests", async ({ delivery, expected }) => {
    const upstreamFetch = vi.fn(async () => new Response("unexpected upstream request"));
    vi.stubGlobal("fetch", upstreamFetch);
    vi.stubGlobal("confirm", vi.fn(() => true));
    installStep17MSendResultFixture(delivery);

    render(<MemoryRouter><WebController /></MemoryRouter>);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Fixture ON を送信" }));
    expect(await screen.findByText((text) => text.includes(expected))).toBeInTheDocument();
    if (delivery === "sent") {
      expect(screen.getByText((text) => text.includes("format=preset_command"))).toBeInTheDocument();
    }
    expect(upstreamFetch).not.toHaveBeenCalled();
  });

  it("blocks every other controller write", async () => {
    const upstreamFetch = vi.fn(async () => new Response("unexpected upstream request"));
    vi.stubGlobal("fetch", upstreamFetch);
    installStep17MSendResultFixture("sent");
    const response = await fetch("/api/controller/rooms/fixture-room", { method: "DELETE" });
    expect(response.status).toBe(405);
    expect(upstreamFetch).not.toHaveBeenCalled();
  });
});
