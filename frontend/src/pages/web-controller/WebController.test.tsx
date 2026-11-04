import { act, render as renderRTL, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import WebController from "./WebController";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function render(ui: ReactElement) {
  return renderRTL(ui, { wrapper: MemoryRouter });
}

const rooms = [
  { id: "bedroom", name: "Bedroom", sort_order: 10 },
  { id: "living", name: "Living", sort_order: 20 },
];

const bedroomDevices = [
  { id: "roomlight", name: "Roomlight", room_id: "bedroom", device_type: "light", primary_adapter: "ir", capabilities: ["send"] },
  { id: "tv", name: "TV", room_id: "bedroom", device_type: "television", primary_adapter: "ir", capabilities: ["send"] },
];

type ControllerFixtureOptions = {
  roomResponse?: unknown;
  failFirstRoomRequest?: boolean;
  failFirstBedroomDeviceRequest?: boolean;
  bedroomDeviceResponse?: unknown;
  livingDeviceResponse?: unknown;
  irRemoteType?: "command" | "stateful";
  failIRConfigRead?: boolean;
  failIRSchemaRead?: boolean;
  commandResponse?: unknown;
  failFirstCommandListRequest?: boolean;
  commandSendResponse?: { body: unknown; status?: number } | (() => Promise<Response>);
  executionOptionsResponse?: unknown;
  stateSchemaResponse?: unknown;
  stateResponse?: unknown;
  stateDetailResponse?: unknown;
  stateSendResponse?: { body: unknown; status?: number } | (() => Promise<Response>);
};

function controllerFetch(options: ControllerFixtureOptions = {}) {
  let roomRequests = 0;
  let bedroomDeviceRequests = 0;
  let commandListRequests = 0;
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    void init;
    const url = String(input);
    if (url === "/api/controller/rooms") {
      roomRequests += 1;
      if (options.failFirstRoomRequest && roomRequests === 1) {
        return jsonResponse({ error: "rooms fixture unavailable" }, 503);
      }
      return jsonResponse(options.roomResponse ?? rooms);
    }
    if (url === "/api/controller/rooms/bedroom/devices") {
      bedroomDeviceRequests += 1;
      if (options.failFirstBedroomDeviceRequest && bedroomDeviceRequests === 1) {
        return jsonResponse({ error: "devices fixture unavailable" }, 503);
      }
      return jsonResponse(options.bedroomDeviceResponse ?? bedroomDevices);
    }
    if (url === "/api/controller/rooms/living/devices") return jsonResponse(options.livingDeviceResponse ?? []);
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/config$/)) {
      if (options.failIRConfigRead) return jsonResponse({ error: "ir config fixture unavailable" }, 503);
      return jsonResponse({ ir_remote_type: options.irRemoteType ?? "command", carrier_khz: 38, default_repeat: 1 });
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/commands$/)) {
      commandListRequests += 1;
      if (options.failFirstCommandListRequest && commandListRequests === 1) {
        return jsonResponse({ error: "ir command fixture unavailable" }, 503);
      }
      const deviceID = url.split("/").at(-3);
      return jsonResponse(options.commandResponse ?? [{ id: "power_on", name: `Power ${deviceID}`, format: "preset_command", pico_command: "LIGHT_ON_CH3" }]);
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/commands\/[^/]+\/send$/)) {
      const response = options.commandSendResponse ?? { body: { ok: true, delivery_state: "sent", requested_repeats: 1, completed_repeats: 1, execution_mode: "preset_command" } };
      if (typeof response === "function") return response();
      return jsonResponse(response.body, response.status ?? 200);
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/execution-options\?/)) {
      return jsonResponse(options.executionOptionsResponse ?? {
        format: "code",
        protocol: "PANASONIC_40",
        execution_modes: {
          backend_raw: { available: true, verification_status: "confirmed", reason: "" },
          native_code: { available: true, verification_status: "confirmed", reason: "" },
        },
      });
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/states\/[^/]+\/send$/)) {
      const response = options.stateSendResponse ?? { body: { ok: true, delivery_state: "sent", requested_repeats: 1, completed_repeats: 1 } };
      if (typeof response === "function") return response();
      return jsonResponse(response.body, response.status ?? 200);
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/states\/[^/]+$/)) {
      return jsonResponse(options.stateDetailResponse ?? {
        id: "cool_26", name: "Cool 26", state: { mode: "cool", power: "on" }, format: "preset_command", pico_command: "AC_COOL_26",
      });
    }
    if (url.match(/^\/api\/controller\/rooms\/bedroom\/devices\/[^/]+\/ir\/states$/)) return jsonResponse(options.stateResponse ?? []);
    if (url === "/api/controller/maintenance/data-health") return jsonResponse({ ok: true, issues: [] });
    if (url.startsWith("/api/controller/maintenance/trash?")) {
      const query = new URLSearchParams(url.split("?")[1]);
      return jsonResponse({ entries: [], total: 0, offset: Number(query.get("offset")), limit: Number(query.get("limit")) });
    }
    if (url === "/api/controller/maintenance/operations") return jsonResponse({ operations: [] });
    if (url.endsWith("/ir/state-schema")) {
      if (options.failIRSchemaRead) return jsonResponse({ error: "schema fixture unavailable" }, 503);
      return options.stateSchemaResponse === undefined
        ? jsonResponse({ error: "ir state schema not found" }, 404)
        : jsonResponse(options.stateSchemaResponse);
    }
    return jsonResponse({ error: `unexpected fixture request: ${url}` }, 500);
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("WebController information hierarchy", () => {
  it("keeps Command and State management controls off Home while retaining daily send actions", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    render(<WebController />);

    expect(await screen.findByRole("button", { name: "Power roomlight を送信" }, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "IR Command作成" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "IR Command編集" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "IR Command削除" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "IR State作成" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "IR State削除" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "学習を開始" })).not.toBeInTheDocument();
    const managementRequests = fetchMock.mock.calls.filter(([, init]) => ["POST", "PUT", "DELETE"].includes(init?.method ?? "GET"));
    expect(managementRequests).toHaveLength(0);
  });

  it("groups Room, Device, daily operations, settings and maintenance without a bedroom-specific title", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    render(<WebController />);

    expect(screen.getByRole("heading", { level: 1, name: "Home Controller" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Bedroom Controller" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual([
      "Room / Device選択",
      "日常操作",
      "保守管理",
    ]);

    const roomDeviceGroup = screen.getByRole("region", { name: "Room / Device選択" });
    expect(within(roomDeviceGroup).getByRole("heading", { level: 3, name: "Rooms" })).toBeInTheDocument();
    expect(within(roomDeviceGroup).getByRole("heading", { level: 3, name: "Devices" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "日常操作" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "保守管理" })).toBeInTheDocument();
    expect(await screen.findByRole("heading", { level: 3, name: "IR Command" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 3, name: "Light" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "状態確認コマンド" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 3, name: "IR Signal" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Room作成" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Room更新" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Room削除" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Device作成" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Device更新" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Device削除" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Room / Device / IR設定" })).toHaveAttribute(
      "href",
      "/settings?room=bedroom&device=roomlight",
    );
    expect(screen.getByRole("link", { name: "Maintenance" })).toHaveAttribute("href", "/maintenance");
    expect(screen.getByRole("link", { name: "Trash" })).toHaveAttribute("href", "/trash");
    expect(screen.queryByRole("heading", { name: "IR Adapter設定" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "設定更新" })).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes("/ir/signals"))).toBe(true);
    expect(fetchMock.mock.calls.every(([url]) => !String(url).includes("/api/controller/maintenance/"))).toBe(true);
    const homeRequests = fetchMock.mock.calls.map(([url]) => String(url));
    expect(homeRequests.every((url) => url === "/api/controller/rooms" || /\/devices(?:\/|$)/.test(url))).toBe(true);
  });

  it("keeps Room selection keyboard-operable and updates the selected Room / Device context", async () => {
    vi.stubGlobal("fetch", controllerFetch());
    const user = userEvent.setup();
    render(<WebController />);

    await screen.findByRole("button", { name: "Bedroom" });
    await waitFor(() => expect(within(screen.getByRole("heading", { name: "Rooms" }).closest("section") as HTMLElement)
      .getByText("Device数").nextElementSibling).toHaveTextContent("2"));
    const roomRefresh = screen.getAllByRole("button", { name: "一覧更新" })[0];
    await user.tab();
    expect(roomRefresh).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "Room Bedroomの情報" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "Bedroom" })).toHaveFocus();
    await user.tab();
    const livingRoom = screen.getByRole("button", { name: "Living" });
    expect(livingRoom).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(livingRoom).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Room Livingの情報" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Room Bedroomの情報" })).not.toBeInTheDocument();
    const emptyDeviceMessage = await screen.findByText(/このRoomにはDeviceがありません/);
    expect(emptyDeviceMessage).toHaveAttribute("role", "status");
    expect(emptyDeviceMessage).toHaveTextContent("Device設定から登録できます。");
    expect(within(screen.getByRole("heading", { name: "Devices" }).closest("section") as HTMLElement)
      .getByRole("link", { name: "Device設定" })).toHaveAttribute("href", "/settings?room=living");
    expect(screen.getAllByText(/このRoomにはDeviceがありません/)).toHaveLength(1);
    expect(within(screen.getByRole("heading", { name: "Rooms" }).closest("section") as HTMLElement)
      .getByText("Device数").nextElementSibling).toHaveTextContent("0");
    expect(screen.queryByText("選択中RoomにはDeviceがまだ登録されていません")).not.toBeInTheDocument();

    await user.tab({ shift: true });
    const bedroomRoom = screen.getByRole("button", { name: "Bedroom" });
    expect(bedroomRoom).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(bedroomRoom).toHaveAttribute("aria-pressed", "true");

    await user.tab({ shift: true });
    expect(screen.getByRole("button", { name: "Room Bedroomの情報" })).toHaveFocus();
    await user.tab();
    expect(bedroomRoom).toHaveFocus();
    await user.tab();
    expect(livingRoom).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(livingRoom).toHaveAttribute("aria-pressed", "true");
  });

  it("keeps Room, Device, and Command info buttons independent from selection and sending", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    const bedroomRoomInfo = await screen.findByRole("button", { name: "Room Bedroomの情報" });
    expect(screen.queryByRole("button", { name: "Room Livingの情報" })).not.toBeInTheDocument();
    await user.click(bedroomRoomInfo);
    expect(screen.getByRole("region", { name: "Bedroom のRoom情報" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bedroom" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Living" })).toHaveAttribute("aria-pressed", "false");
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/controller/rooms/living/devices")).toBe(false);

    const tvInfo = await screen.findByRole("button", { name: "TV (tv)の情報" });
    await user.click(tvInfo);
    expect(screen.getByRole("region", { name: "TV のDevice情報" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "TV のDevice情報" })).toHaveTextContent("IR Deviceを選択すると、下の「日常操作」が選択内容に合わせて切り替わります。");
    expect(screen.getByRole("button", { name: "TV (tv) を選択" })).toHaveAttribute("aria-pressed", "false");

    const commandInfo = await screen.findByRole("button", { name: "Command Power roomlight (power_on)の情報" });
    const powerCommand = screen.getByRole("button", { name: /Power roomlight \(power_on\).*送信可能/ });
    expect(powerCommand.parentElement).toContainElement(commandInfo);
    const priorSelection = powerCommand.getAttribute("aria-pressed");
    await user.click(commandInfo);
    expect(screen.getByRole("region", { name: "Power roomlight のCommand情報" })).toBeInTheDocument();
    expect(powerCommand).toHaveAttribute("aria-pressed", priorSelection);
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/send"))).toBe(false);
  });

  it("selects a Command through its card while its information button does not select it", async () => {
    const fetchMock = controllerFetch({ commandResponse: [
      { id: "power_on", name: "Power On", format: "preset_command", pico_command: "LIGHT_ON_CH3" },
      { id: "power_off", name: "Power Off", format: "preset_command", pico_command: "LIGHT_OFF_CH3" },
    ] });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    const card = await screen.findByRole("button", { name: /Power Off \(power_off\).*送信可能/ });
    const info = screen.getByRole("button", { name: "Command Power Off (power_off)の情報" });
    expect(card.parentElement).toContainElement(info);
    await user.click(info);
    expect(card).toHaveAttribute("aria-pressed", "false");
    await user.click(card);
    expect(card).toHaveAttribute("aria-pressed", "true");
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/send"))).toBe(false);
  });

  it("opens the Room from the route and loads only that Room's Device list", async () => {
    const fetchMock = controllerFetch({
      livingDeviceResponse: [
        { id: "lamp", name: "Living Lamp", room_id: "living", device_type: "light", primary_adapter: "ir" },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);
    const { rerender } = render(<WebController routeRoomId="living" />);

    const livingRoom = await screen.findByRole("button", { name: "Living" });
    expect(livingRoom).toHaveAttribute("aria-pressed", "true");
    const selectedLamp = await screen.findByRole("button", { name: "Living Lamp (lamp) を選択" });
    expect(selectedLamp).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findAllByText("Living (living)")).not.toHaveLength(0);
    const lampCard = selectedLamp.closest("article");
    expect(lampCard).not.toBeNull();
    expect(within(lampCard as HTMLElement).getByRole("heading", { name: "Living Lamp" })).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/controller/rooms/living/devices")).toBe(true);
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/controller/rooms/bedroom/devices")).toBe(false);

    rerender(<WebController routeRoomId="bedroom" />);
    expect(await screen.findByRole("button", { name: "Bedroom" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findByRole("button", { name: "TV (tv) を選択" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("button", { name: "Living Lamp (lamp) を選択" })).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/controller/rooms/bedroom/devices")).toBe(true);
  });

  it("does not let a late Device response from the previous Room replace the current selection", async () => {
    let resolveLivingDevices!: (response: Response) => void;
    const pendingLivingDevices = new Promise<Response>((resolve) => {
      resolveLivingDevices = resolve;
    });
    const fallbackFetch = controllerFetch({
      livingDeviceResponse: [
        { id: "lamp", name: "Living Lamp", room_id: "living", device_type: "light", primary_adapter: "ir" },
      ],
    });
    const fetchMock = vi.fn((input: RequestInfo | URL) =>
      String(input) === "/api/controller/rooms/living/devices"
        ? pendingLivingDevices
        : fallbackFetch(input)
    );
    vi.stubGlobal("fetch", fetchMock);
    const { rerender } = render(<WebController routeRoomId="living" />);

    await vi.waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/controller/rooms/living/devices")).toBe(true)
    );
    rerender(<WebController routeRoomId="bedroom" />);
    expect(await screen.findByRole("button", { name: "TV (tv) を選択" })).toHaveAttribute("aria-pressed", "false");

    await act(async () => {
      resolveLivingDevices(jsonResponse([
        { id: "lamp", name: "Living Lamp", room_id: "living", device_type: "light", primary_adapter: "ir" },
      ]));
      await pendingLivingDevices;
    });

    expect(screen.queryByRole("button", { name: "Living Lamp (lamp) を選択" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bedroom" })).toHaveAttribute("aria-pressed", "true");
  });

  it("does not silently select another Room for an unknown direct URL and recovers by choosing a listed Room", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    const onRoomSelect = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(<WebController routeRoomId="missing-room" onRoomSelect={onRoomSelect} />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Roomが見つかりません: missing-room");
    expect(within(screen.getByRole("heading", { name: "Rooms" }).closest("section") as HTMLElement)
      .getByText("Device数").nextElementSibling).toHaveTextContent("-");
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/rooms/missing-room/devices"))).toBe(false);

    await user.click(screen.getByRole("button", { name: "Living" }));
    expect(onRoomSelect).toHaveBeenCalledWith("living");
    rerender(<WebController routeRoomId="living" onRoomSelect={onRoomSelect} />);
    expect(screen.getByRole("button", { name: "Living" })).toHaveAttribute("aria-pressed", "true");
  });

  it("separates Room loading errors from the empty state and retries through the existing refresh action", async () => {
    vi.stubGlobal("fetch", controllerFetch({ roomResponse: [], failFirstRoomRequest: true }));
    const user = userEvent.setup();
    render(<WebController />);

    expect(await screen.findByText("rooms fixture unavailable")).toHaveAttribute("role", "alert");
    expect(screen.queryByText(/Roomはまだ登録されていません/)).not.toBeInTheDocument();

    await user.click(screen.getAllByRole("button", { name: "一覧更新" })[0]);
    const emptyRoomMessage = await screen.findByText(/Roomはまだ登録されていません/);
    expect(emptyRoomMessage).toHaveAttribute("role", "status");
    expect(emptyRoomMessage).toHaveTextContent("Room設定から作成できます。");
  });

  it("shows a loading status instead of Room empty guidance while the first read is pending", async () => {
    let resolveRooms!: (response: Response) => void;
    const pendingRooms = new Promise<Response>((resolve) => {
      resolveRooms = resolve;
    });
    const fallbackFetch = controllerFetch();
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input) === "/api/controller/rooms") return pendingRooms;
      return fallbackFetch(input);
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<WebController />);

    const roomSection = screen.getByRole("heading", { name: "Rooms" }).closest("section");
    expect(roomSection).not.toBeNull();
    expect(within(roomSection as HTMLElement).getByText("Room一覧を読み込んでいます...")).toHaveAttribute("role", "status");
    expect(within(roomSection as HTMLElement).queryByText(/Roomはまだ登録されていません/)).not.toBeInTheDocument();

    await act(async () => {
      resolveRooms(jsonResponse([]));
      await pendingRooms;
    });
    expect(await within(roomSection as HTMLElement).findByText(/Roomはまだ登録されていません/)).toBeInTheDocument();
  });

  it("separates Device loading errors from an empty Device list and exposes the next action once", async () => {
    vi.stubGlobal("fetch", controllerFetch({
      failFirstBedroomDeviceRequest: true,
      bedroomDeviceResponse: [],
    }));
    const user = userEvent.setup();
    render(<WebController />);

    expect(await screen.findByText("devices fixture unavailable")).toHaveAttribute("role", "alert");
    expect(screen.queryByText(/このRoomにはDeviceがありません/)).not.toBeInTheDocument();

    const deviceSection = screen.getByRole("heading", { name: "Devices" }).closest("section");
    expect(deviceSection).not.toBeNull();
    await user.click(within(deviceSection as HTMLElement).getByRole("button", { name: "一覧更新" }));

    const emptyDeviceMessage = await screen.findByText(/このRoomにはDeviceがありません/);
    expect(emptyDeviceMessage).toHaveAttribute("role", "status");
    expect(emptyDeviceMessage).toHaveTextContent("Device設定から登録できます。");
    expect(within(deviceSection as HTMLElement).getAllByRole("link", { name: "Device設定" })).toHaveLength(1);
  });

  it("reports IR Command send delivery without claiming the appliance's actual state", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    const commandHeading = await screen.findByRole("heading", { name: "IR Command" });
    const commandSection = commandHeading.closest("section");
    expect(commandSection).not.toBeNull();
    await screen.findByText("Power roomlight");
    await user.click(within(commandSection as HTMLElement).getByRole("button", { name: "Power roomlight を送信" }));

    const result = await within(commandSection as HTMLElement).findByText(/送信成功/);
    expect(result).toHaveAttribute("role", "status");
    expect(result).toHaveTextContent("delivery=sent");
    expect(within(commandSection as HTMLElement).getByText(/家電の現在状態は取得しません/)).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([request]) => String(request).endsWith("/ir/commands/power_on/send"))).toBe(true);
  });

  it("shows the selected operation as sending and prevents changing the target until the request ends", async () => {
    let resolveSend!: (response: Response) => void;
    const pendingResponse = new Promise<Response>((resolve) => { resolveSend = resolve; });
    const fetchMock = controllerFetch({ commandSendResponse: () => pendingResponse });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    const sendButton = await screen.findByRole("button", { name: "Power roomlight を送信" }, { timeout: 5000 });
    await user.click(sendButton);
    expect(await screen.findByText("送信中: Power roomlight")).toHaveAttribute("role", "status");
    expect(sendButton).toBeDisabled();
    expect(within(await screen.findByRole("group", { name: "IR Command操作" }, { timeout: 5000 })).getByRole("button", { name: /Power roomlight/, pressed: true })).toBeDisabled();

    await act(async () => {
      resolveSend(jsonResponse({ ok: true, delivery_state: "sent", requested_repeats: 1, completed_repeats: 1 }));
      await pendingResponse;
    });
    expect(await screen.findByText(/送信成功: Power roomlight/)).toHaveAttribute("role", "status");
  });

  it("presents the six Roomlight commands as an explicit operation button group", async () => {
    const roomlightCommands = [
      ["bright_down_ch3", "BRIGHT_DOWN_CH3", "LIGHT_BRIGHT_DOWN_CH3"],
      ["bright_up_ch3", "BRIGHT_UP_CH3", "LIGHT_BRIGHT_UP_CH3"],
      ["full_ch3", "FULL_CH3", "LIGHT_FULL_CH3"],
      ["night_ch3", "NIGHT_CH3", "LIGHT_NIGHT_CH3"],
      ["off_ch3", "OFF_CH3", "LIGHT_OFF_CH3"],
      ["on_ch3", "ON_CH3", "LIGHT_ON_CH3"],
    ].map(([id, name, pico_command]) => ({ id, name, format: "preset_command", pico_command }));
    const fetchMock = controllerFetch({ commandResponse: roomlightCommands });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    const commandGroup = await screen.findByRole("group", { name: "IR Command操作" }, { timeout: 5000 });
    const buttons = within(commandGroup).getAllByRole("button").filter((button) => button.hasAttribute("aria-pressed"));
    expect(buttons).toHaveLength(6);
    expect(buttons.map((button) => button.firstElementChild?.textContent)).toEqual([
      "BRIGHT_DOWN_CH3",
      "BRIGHT_UP_CH3",
      "FULL_CH3",
      "NIGHT_CH3",
      "OFF_CH3",
      "ON_CH3",
    ]);
    expect(screen.getAllByText("Bedroom (bedroom)").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Roomlight (roomlight)").length).toBeGreaterThan(0);

    const onButton = within(commandGroup).getByRole("button", { name: /ON_CH3/, pressed: false });
    await user.click(onButton);
    expect(onButton).toHaveAttribute("aria-pressed", "true");
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/send"))).toBe(false);
    await user.click(screen.getByRole("button", { name: "ON_CH3 を送信" }));
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/commands/on_ch3/send"))).toBe(true);
  });

  it("preserves all four Command formats, blocks unlearned or incompatible RAW, and requires a human code-mode choice", async () => {
    const commands = [
      { id: "preset", name: "Preset On", format: "preset_command", pico_command: "LIGHT_ON_CH3" },
      { id: "raw", name: "RAW 40kHz", format: "raw", carrier_khz: 40, timings_us: [500, 500], count: 2 },
      { id: "code", name: "Code On", format: "code", protocol: "PANASONIC_40", code: "344A90BC2C" },
      { id: "samples", name: "Unlearned Samples", format: "raw_samples", count: 0, captures: [], timings_us: [] },
    ];
    const fetchMock = controllerFetch({
      commandResponse: commands,
      executionOptionsResponse: {
        format: "code",
        protocol: "PANASONIC_40",
        execution_modes: {
          backend_raw: { available: false, verification_status: "unknown", reason: "not verified" },
          native_code: { available: true, verification_status: "confirmed", reason: "" },
        },
      },
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    const commandGroup = await screen.findByRole("group", { name: "IR Command操作" }, { timeout: 5000 });
    const operationButtons = within(commandGroup).getAllByRole("button").filter((button) => button.hasAttribute("aria-pressed"));
    for (const id of ["preset", "raw", "code", "samples"]) {
      expect(operationButtons.some((button) => button.getAttribute("aria-label")?.includes(`(${id})`))).toBe(true);
    }

    await user.click(within(commandGroup).getByRole("button", { name: /Unlearned Samples/, pressed: false }));
    expect(screen.getByText(/未学習のraw_samples Commandは送信できません/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unlearned Samples を送信" })).toBeDisabled();

    await user.click(within(commandGroup).getByRole("button", { name: /RAW 40kHz/, pressed: false }));
    expect(screen.getByText(/実効carrierは40kHz/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "RAW 40kHz を送信" })).toBeDisabled();

    await user.click(within(commandGroup).getByRole("button", { name: /Code On/, pressed: false }));
    const executionMode = await screen.findByRole("combobox", { name: "execution mode" });
    expect(executionMode).toHaveValue("");
    expect(screen.getByRole("button", { name: "Code On を送信" })).toBeDisabled();
    expect(within(executionMode).getByRole("option", { name: "backend_raw" })).toBeDisabled();
    expect(screen.getByText(/backend_raw: 利用不可/)).toBeInTheDocument();
    await user.selectOptions(executionMode, "native_code");
    expect(screen.getByRole("button", { name: "Code On を送信" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Code On を送信" }));
    const sendRequest = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/commands/code/send"));
    expect(sendRequest).toBeDefined();
    const [, commandSendInit] = sendRequest as unknown as [RequestInfo | URL, RequestInit];
    expect(JSON.parse(String(commandSendInit.body))).toEqual({ execution_mode: "native_code" });
  });

  it("shows a command repeat field error without sending", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    await screen.findByRole("button", { name: "Power roomlight を送信" }, { timeout: 5000 });
    const repeat = document.querySelector<HTMLInputElement>("#ir-command-send-repeat");
    expect(repeat).not.toBeNull();
    await user.clear(repeat as HTMLInputElement);
    await user.type(repeat as HTMLInputElement, "9");
    await user.click(screen.getByRole("button", { name: "Power roomlight を送信" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("repeatは空欄または1から5の範囲で指定してください");
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/commands/power_on/send"))).toBe(false);
  });

  it.each([
    { delivery: "not_sent", guidance: "送信は開始されていません" },
    { delivery: "partially_sent", guidance: "1回中0回の成功を確認しました" },
    { delivery: "unknown", guidance: "送信済みの可能性があります" },
  ])("explains $delivery Command delivery errors without automatic retries", async ({ delivery, guidance }) => {
    const fetchMock = controllerFetch({
      commandSendResponse: {
        status: 502,
        body: {
          ok: false,
          error: "controller send failed",
          detail: "fixture delivery error",
          protocol: "",
          execution_mode: "",
          delivery_state: delivery,
          requested_repeats: 1,
          completed_repeats: 0,
          retryable_with: [],
        },
      },
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    await user.click(await screen.findByRole("button", { name: "Power roomlight を送信" }, { timeout: 5000 }));
    expect(await screen.findByRole("alert")).toHaveTextContent(guidance);
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/commands/power_on/send"))).toHaveLength(1);
  });

  it.each([
    { delivery: "sent", status: 200, guidance: "実機の現在状態は取得しません" },
    { delivery: "not_sent", status: 500, guidance: "送信は開始されていません" },
    { delivery: "partially_sent", status: 500, guidance: "2回中1回の成功を確認しました" },
    { delivery: "unknown", status: 500, guidance: "送信済みの可能性があります" },
  ])("reports $delivery for a learned raw_samples AC State without automatically resending", async ({ delivery, status, guidance }) => {
    const state = {
      id: "cool_26_auto", name: "冷房26℃自動", format: "raw_samples",
      state: { power: "on", mode: "cool", target_temperature: 26, fan: "auto", swing: "auto" },
      count: 10, timings_us: [9000, 4500, ...Array(8).fill(560)],
      captures: [{ valid: true, count: 10, timings_us: [9000, 4500, ...Array(8).fill(560)] }],
    };
    const fetchMock = controllerFetch({
      bedroomDeviceResponse: [{ id: "bedroom_ac", name: "寝室エアコン", room_id: "bedroom", device_type: "air_conditioner", primary_adapter: "ir" }],
      irRemoteType: "stateful",
      stateSchemaResponse: {
        power: { type: "enum", values: ["on", "off"] },
        mode: { type: "enum", values: ["cool", "heat"] },
        target_temperature: { type: "number", min: 16, max: 30 },
        fan: { type: "enum", values: ["auto", "low"] },
        swing: { type: "enum", values: ["auto", "fixed"] },
      },
      stateResponse: [state], stateDetailResponse: state,
      stateSendResponse: {
        status,
        body: { ok: status === 200, state_id: state.id, error: status === 200 ? undefined : "controller daemon error", delivery_state: delivery, requested_repeats: 2, completed_repeats: delivery === "sent" ? 2 : delivery === "partially_sent" ? 1 : 0, retryable_with: [] },
      },
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);
    const send = await screen.findByRole("button", { name: "冷房26℃自動 を送信" });
    expect(send).toBeEnabled();
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/cool_26_auto/send"))).toHaveLength(0);
    await user.click(send);
    if (status === 200) {
      expect(await screen.findByText(/IR State送信を完了しました/)).toHaveTextContent(guidance);
    } else {
      expect(await screen.findByRole("alert")).toHaveTextContent(guidance);
      expect(screen.queryByText(/IR State送信を完了しました/)).not.toBeInTheDocument();
    }
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/cool_26_auto/send"))).toHaveLength(1);
  });

  it("keeps Stateful Schema values on State operation buttons and validates repeat as a field", async () => {
    const state = { id: "cool_26", name: "Cool 26", state: { mode: "cool", power: "on" }, format: "preset_command", pico_command: "AC_COOL_26" };
    const fetchMock = controllerFetch({
      irRemoteType: "stateful",
      stateSchemaResponse: {
        mode: { type: "enum", values: ["cool", "heat"] },
        power: { type: "enum", values: ["on", "off"] },
      },
      stateResponse: [state],
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    const stateGroup = await screen.findByRole("group", { name: "IR State操作" }, { timeout: 5000 });
    const stateButton = within(stateGroup).getByRole("button", { name: /mode=cool.*power=on/ });
    expect(stateButton).toHaveAttribute("aria-pressed", "true");
    await user.click(within(stateGroup).getByRole("button", { name: "State Cool 26 (cool_26)の情報" }));
    expect(screen.getByRole("region", { name: "Cool 26 のState情報" })).toBeInTheDocument();
    expect(stateButton).toHaveAttribute("aria-pressed", "true");
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/states/cool_26/send"))).toBe(false);
    expect(stateButton).toHaveAccessibleName(/mode=cool.*power=on/);

    const repeat = screen.getByLabelText("リピート回数 (repeat)");
    await user.clear(repeat);
    await user.type(repeat, "8");
    await user.click(screen.getByRole("button", { name: "Cool 26 を送信" }));
    expect(await screen.findByText("repeatは1から5の範囲で指定してください")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/states/cool_26/send"))).toBe(false);
  });

  it("requires an explicit available execution mode for a Stateful code State", async () => {
    const state = { id: "panasonic_on", name: "Panasonic On", state: { power: "on" }, format: "code", protocol: "PANASONIC_40", code: "344A90BC2C" };
    const fetchMock = controllerFetch({
      irRemoteType: "stateful",
      stateSchemaResponse: { power: { type: "enum", values: ["on", "off"] } },
      stateResponse: [state],
      executionOptionsResponse: {
        format: "code",
        protocol: "PANASONIC_40",
        execution_modes: {
          backend_raw: { available: true, verification_status: "confirmed", reason: "" },
          native_code: { available: true, verification_status: "confirmed", reason: "" },
        },
      },
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    render(<WebController />);

    const sendButton = await screen.findByRole("button", { name: "Panasonic On を送信" }, { timeout: 5000 });
    const executionMode = await screen.findByRole("combobox", { name: "execution mode" }, { timeout: 5000 });
    expect(executionMode).toHaveValue("");
    expect(sendButton).toBeDisabled();
    await user.selectOptions(executionMode, "backend_raw");
    expect(sendButton).toBeEnabled();
    await user.click(sendButton);
    const sendRequest = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/states/panasonic_on/send"));
    expect(sendRequest).toBeDefined();
    const [, stateSendInit] = sendRequest as unknown as [RequestInfo | URL, RequestInit];
    expect(JSON.parse(String(stateSendInit.body))).toEqual({ execution_mode: "backend_raw" });
  });

  it("renders one actionable empty state for a successfully loaded IR Command list", async () => {
    vi.stubGlobal("fetch", controllerFetch({ commandResponse: [] }));
    render(<WebController />);

    const emptyCommandMessage = await screen.findByText(/IR Commandはまだ登録されていません/);
    expect(emptyCommandMessage).toHaveAttribute("role", "status");
    expect(emptyCommandMessage).toHaveTextContent("Settings");
    expect(screen.getAllByText(/IR Commandはまだ登録されていません/)).toHaveLength(1);
  });

  it("keeps Command list errors separate from empty state and retries the existing read", async () => {
    const fetchMock = controllerFetch({ failFirstCommandListRequest: true });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    const commandHeading = await screen.findByRole("heading", { name: "IR Command" }, { timeout: 5000 });
    const commandSection = commandHeading.closest("section");
    expect(commandSection).not.toBeNull();
    expect(await within(commandSection as HTMLElement).findByRole("alert", {}, { timeout: 5000 })).toHaveTextContent("ir command fixture unavailable");
    expect(within(commandSection as HTMLElement).queryByText(/IR Commandはまだ登録されていません/)).not.toBeInTheDocument();

    await user.click(within(commandSection as HTMLElement).getByRole("button", { name: "一覧更新" }));
    expect(await within(commandSection as HTMLElement).findByRole("button", { name: "Power roomlight を送信" }, { timeout: 5000 })).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/ir/commands"))).toHaveLength(2);
  });

  it("shows stateful Schema and State empty guidance only after their reads finish", async () => {
    vi.stubGlobal("fetch", controllerFetch({ irRemoteType: "stateful" }));
    render(<WebController />);

    expect(screen.queryByText(/State Schemaはまだ作成されていません/)).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
    expect(await screen.findByText(/IR Stateはまだ登録されていません/, {}, { timeout: 5000 })).toHaveAttribute("role", "status");
    expect(screen.getByRole("link", { name: "Settings" })).toBeInTheDocument();
  });

  it("blocks IR configuration edits and Command assumptions when configuration could not be read", async () => {
    vi.stubGlobal("fetch", controllerFetch({ failIRConfigRead: true }));
    render(<WebController />);

    expect(screen.queryByRole("heading", { name: "IR Adapter設定" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "設定更新" })).not.toBeInTheDocument();

    const commandSection = (await screen.findByRole("heading", { name: "IR Command" }, { timeout: 5000 })).closest("section");
    expect(commandSection).not.toBeNull();
    expect(await within(commandSection as HTMLElement).findByRole("alert", {}, { timeout: 5000 })).toHaveTextContent("Command / Stateを判定できません");
    expect(within(commandSection as HTMLElement).queryByText(/IR Commandはまだ登録されていません/)).not.toBeInTheDocument();
  });

  it("does not mount State Schema settings on Home when the Schema read fails", async () => {
    vi.stubGlobal("fetch", controllerFetch({ irRemoteType: "stateful", failIRSchemaRead: true }));
    render(<WebController />);

    await screen.findByRole("heading", { name: "IR Command" }, { timeout: 5000 });
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Schema更新" })).not.toBeInTheDocument();
  });

  it("selects a Device and shows its operation panel context without invoking device actions", async () => {
    const fetchMock = controllerFetch();
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<WebController />);

    const tvHeading = await screen.findByRole("heading", { level: 4, name: "TV" });
    const tvCard = tvHeading.closest("article");
    expect(tvCard).not.toBeNull();
    const selectTV = within(tvCard as HTMLElement).getByRole("button", { name: "TV (tv) を選択" });
    selectTV.focus();
    expect(selectTV).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(selectTV).toHaveAttribute("aria-pressed", "true");

    const commandHeading = await screen.findByRole("heading", { level: 3, name: "IR Command" });
    const commandSection = commandHeading.closest("section");
    expect(commandSection).not.toBeNull();
    expect(await within(commandSection as HTMLElement).findByText("TV (tv)")).toBeInTheDocument();
    expect(await within(commandSection as HTMLElement).findByText("Power tv")).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([request]) => String(request).endsWith("/ir/commands/tv/send"))).toBe(false);
  });
});

describe("Step 4E conditional daily State operations", () => {
  const schema = {
    enabled: { type: "boolean" as const, label: "動作" },
    profile: { type: "enum" as const, values: ["normal", "boost"], label: "設定" },
    level: { type: "number" as const, presence: "absent" as const, cases: [{ when: { all: [{ field: "enabled", in: [true] }, { field: "profile", in: ["normal"] }] }, presence: "required" as const, min: 0, max: 5, step: 1, label: "強さ", unit: "段" }] },
  };
  it("uses resolved labels/units for Home and send confirmation without sending on selection/refresh", async () => {
    const state = { id: "normal", name: "通常", state: { enabled: true, profile: "normal", level: 0 }, format: "raw_samples", count: 2, timings_us: [100, 200], captures: [] };
    const fetchMock = controllerFetch({ irRemoteType: "stateful", stateSchemaResponse: schema, stateResponse: [state], stateDetailResponse: state });
    vi.stubGlobal("fetch", fetchMock); const confirm = vi.fn(() => true); vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup(); render(<WebController />);
    const group = await screen.findByRole("group", { name: "IR State操作" });
    const button = await within(group).findByRole("button", { name: /動作=true.*設定=normal.*強さ=0 段/ });
    await user.click(button);
    const section = group.closest("section")!;
    await user.click(within(section).getByRole("button", { name: "一覧更新" }));
    await waitFor(() => expect(within(section).getByRole("button", { name: "通常 を送信" })).toBeEnabled());
    const sends = () => fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/normal/send"));
    expect(sends()).toHaveLength(0);
    await user.click(within(section).getByRole("button", { name: "通常 を送信" }));
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining("強さ=0 段"));
    await waitFor(() => expect(sends()).toHaveLength(1));
  });
  it.each([
    { value: { enabled: false, profile: "normal", level: 0 }, label: "保存State不整合", learned: true },
    { value: { enabled: true, profile: "normal", level: 0 }, label: "未学習", learned: false },
  ])("blocks $label before any send request", async ({ value, label, learned }) => {
    const state = { id: "blocked", name: "確認対象", state: value, format: "raw_samples", count: learned ? 2 : 0, timings_us: learned ? [100, 200] : [], captures: [] };
    const fetchMock = controllerFetch({ irRemoteType: "stateful", stateSchemaResponse: schema, stateResponse: [state], stateDetailResponse: state });
    vi.stubGlobal("fetch", fetchMock); const user = userEvent.setup(); render(<WebController />);
    const button = await screen.findByRole("button", { name: "確認対象 を送信" });
    expect(button).toBeDisabled();
    const group = screen.getByRole("group", { name: "IR State操作" });
    expect(within(group).getByRole("button", { name: /確認対象 \(blocked\)/, pressed: true })).toHaveAccessibleName(new RegExp(label));
    await user.click(button); expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/states/blocked/send"))).toBe(false);
    if (label === "保存State不整合") expect(within(group).getByRole("button", { name: /確認対象 \(blocked\)/, pressed: true })).not.toHaveAccessibleName(/強さ=0/);
  });
  it("shows Backend Schema conflict issues and keeps a single send request without retry", async () => {
    const state = { id: "conflict", name: "競合", state: { enabled: true, profile: "normal", level: 0 }, format: "raw_samples", count: 2, timings_us: [100, 200], captures: [] };
    const fetchMock = controllerFetch({ irRemoteType: "stateful", stateSchemaResponse: schema, stateResponse: [state], stateDetailResponse: state, stateSendResponse: { status: 409, body: { ok: false, error: "ir state does not match schema", detail: "", delivery_state: "not_sent", requested_repeats: 1, completed_repeats: 0, retryable_with: [], issues: [{ field_id: "level", code: "range", message: "fixture changed range", case_index: 0 }] } } });
    vi.stubGlobal("fetch", fetchMock); vi.stubGlobal("confirm", vi.fn(() => true)); const user = userEvent.setup(); render(<WebController />);
    const send = await screen.findByRole("button", { name: "競合 を送信" }); await waitFor(() => expect(send).toBeEnabled()); await user.click(send);
    expect(await screen.findByText(/level case 1: range fixture changed range/)).toBeInTheDocument();
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/conflict/send"))).toHaveLength(1);
  });

});
