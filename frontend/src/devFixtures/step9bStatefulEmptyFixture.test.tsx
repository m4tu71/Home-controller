import { render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";

import WebController from "../pages/web-controller/WebController";
import RoomDeviceSettingsPage from "../pages/web-controller/RoomDeviceSettingsPage";
import { installStep9BStatefulEmptyFixture } from "./step9bStatefulEmptyFixture";

describe("Step 9B browser fixture", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("serves a Stateful Device with empty States and never forwards controller API calls", async () => {
    const upstreamFetch = vi.fn(async () => new Response("unexpected network call"));
    vi.stubGlobal("fetch", upstreamFetch);
    installStep9BStatefulEmptyFixture();

    const roomsResponse = await fetch("/api/controller/rooms");
    const rooms = await roomsResponse.json();
    expect(rooms).toEqual([{ id: "fixture-room", name: "Fixture Room", sort_order: 1 }]);

    const configResponse = await fetch(
      "/api/controller/rooms/fixture-room/devices/fixture-stateful-ac/ir/config",
    );
    expect(await configResponse.json()).toMatchObject({ ir_remote_type: "stateful" });

    const schemaResponse = await fetch(
      "/api/controller/rooms/fixture-room/devices/fixture-stateful-ac/ir/state-schema",
    );
    expect(schemaResponse.status).toBe(404);

    const statesResponse = await fetch(
      "/api/controller/rooms/fixture-room/devices/fixture-stateful-ac/ir/states",
    );
    expect(await statesResponse.json()).toEqual([]);

    const sendResponse = await fetch(
      "/api/controller/rooms/fixture-room/devices/fixture-stateful-ac/ir/states/power/send",
      { method: "POST" },
    );
    expect(sendResponse.status).toBe(405);
    expect(upstreamFetch).not.toHaveBeenCalled();
  });

  it("renders the IR State empty guidance in the browser fixture", async () => {
    const upstreamFetch = vi.fn(async () => new Response("unexpected network call"));
    vi.stubGlobal("fetch", upstreamFetch);
    installStep9BStatefulEmptyFixture();

    render(
      <MemoryRouter>
        <WebController />
      </MemoryRouter>,
    );

    const stateHeading = await screen.findByRole(
      "heading",
      { name: "IR State" },
      { timeout: 5000 },
    );
    const stateSection = stateHeading.closest("section");
    expect(stateSection).not.toBeNull();
    expect(await within(stateSection as HTMLElement).findByText(/IR Stateはまだ登録されていません/)).toBeInTheDocument();
    expect(within(stateSection as HTMLElement).getByRole("link", { name: "Settings" })).toHaveAttribute(
      "href",
      "/settings?room=fixture-room&device=fixture-stateful-ac",
    );
    expect(screen.queryByText(/State Schemaはまだ作成されていません/)).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
    expect(upstreamFetch).not.toHaveBeenCalled();
  });

  it("renders the missing State Schema guidance only on the Settings route", async () => {
    const upstreamFetch = vi.fn(async () => new Response("unexpected network call"));
    vi.stubGlobal("fetch", upstreamFetch);
    installStep9BStatefulEmptyFixture();

    render(
      <MemoryRouter initialEntries={["/settings?room=fixture-room&device=fixture-stateful-ac"]}>
        <Routes>
          <Route path="/settings" element={<RoomDeviceSettingsPage />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText(/State Schemaはまだ作成されていません/)).toBeInTheDocument();
    expect(await screen.findByText(/既存Stateはありません/)).toBeInTheDocument();
    expect(upstreamFetch).not.toHaveBeenCalled();
  });
});
