import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { IRStateSchema, IRStateSchemaIssue } from "../../api/controller";
import RoomDeviceSettingsPage from "./RoomDeviceSettingsPage";

type RoomFixture = { id: string; name: string; sort_order?: number };
type DeviceFixture = {
  id: string;
  name: string;
  room_id: string;
  device_type: string;
  primary_adapter: string;
  capabilities?: string[];
};

type SettingsFixtureOptions = {
  bedroomDevices?: DeviceFixture[];
  irRemoteType?: "command" | "stateful";
  missingIRConfig?: boolean;
  failIRConfigRead?: boolean;
  failIRSchemaRead?: boolean;
  stateSchema?: IRStateSchema;
  configUpdateStatus?: number;
  schemaUpdateStatus?: number;
  schemaIssues?: IRStateSchemaIssue[];
  commands?: Array<Record<string, unknown>>;
  states?: Array<Record<string, unknown>>;
  stateSchemaForManagement?: IRStateSchema;
  stateCaptureResponse?: { body: unknown; status: number };
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function createSettingsFixture(options: SettingsFixtureOptions = {}) {
  let savedSchema = options.stateSchemaForManagement ?? options.stateSchema;
  let rooms: RoomFixture[] = [
    { id: "bedroom", name: "Bedroom", sort_order: 10 },
    { id: "living", name: "Living", sort_order: 20 },
  ];
  const devices: Record<string, DeviceFixture[]> = {
    bedroom: options.bedroomDevices ?? [
      { id: "roomlight", name: "Roomlight", room_id: "bedroom", device_type: "light", primary_adapter: "ir" },
      { id: "tv", name: "TV", room_id: "bedroom", device_type: "television", primary_adapter: "ir" },
    ],
    living: [],
  };
  let commands = options.commands ?? [];
  let states = options.states ?? [];
  let failNextMutation = false;
  let holdNextMutation = false;
  let releaseHeldMutation: ((response: Response) => void) | undefined;
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : {};

    const configMatch = url.match(/^\/api\/controller\/rooms\/([^/]+)\/devices\/([^/]+)\/ir\/config$/);
    if (configMatch) {
      if (method === "GET") {
        if (options.failIRConfigRead) return jsonResponse({ error: "IR設定 fixture unavailable" }, 503);
        if (options.missingIRConfig) return jsonResponse({ error: "IR設定が見つかりません" }, 404);
        return jsonResponse({ ir_remote_type: options.irRemoteType ?? "command", carrier_khz: 38, default_repeat: 1 });
      }
      if (method === "PUT") {
        if (options.configUpdateStatus) return jsonResponse({ error: "IR config conflict" }, options.configUpdateStatus);
        savedSchema = body as IRStateSchema;
        return jsonResponse(body);
      }
    }

    if (url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/commands$/)) {
      if (method === "GET") return jsonResponse(commands);
      if (method === "POST") {
        const command = body;
        commands = [...commands, command];
        return jsonResponse(command, 201);
      }
    }

    const commandCaptureMatch = url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/commands\/([^/]+)\/capture$/);
    if (commandCaptureMatch && method === "POST") {
      const commandID = decodeURIComponent(commandCaptureMatch[1]);
      commands = commands.map((command) => command.id === commandID ? { ...command, count: 4, timings_us: [9000, 4500, 560, 560] } : command);
      return jsonResponse({ ok: true, command: commands.find((command) => command.id === commandID), required_samples: 1, valid_captures: 1 });
    }
    const commandMatch = url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/commands\/([^/]+)$/);
    if (commandMatch) {
      const commandID = decodeURIComponent(commandMatch[1]);
      if (method === "PUT") {
        const updatedCommand = { ...commands.find((command) => command.id === commandID), ...body, id: commandID };
        commands = commands.map((command) => command.id === commandID ? updatedCommand : command);
        return jsonResponse(updatedCommand);
      }
      if (method === "DELETE") {
        commands = commands.filter((command) => command.id !== commandID);
        return jsonResponse({ ok: true, command_id: commandID, trash_entry_id: "trash_command_1", operation_id: "delete_command_1", status: "verified" });
      }
    }

    const schemaMatch = url.match(/^\/api\/controller\/rooms\/([^/]+)\/devices\/([^/]+)\/ir\/state-schema$/);
    if (schemaMatch) {
      if (method === "GET") {
        if (options.failIRSchemaRead) return jsonResponse({ error: "schema fixture unavailable" }, 503);
        const schema = savedSchema;
        if (schema === undefined) return jsonResponse({ error: "ir state schema not found" }, 404);
        return jsonResponse(schema);
      }
      if (method === "PUT") {
        if (options.schemaUpdateStatus) {
          return jsonResponse({ error: "Schema conflicts with existing State", invalid_state_ids: ["cool_26"], issues: options.schemaIssues }, options.schemaUpdateStatus);
        }
        savedSchema = body as IRStateSchema;
        return jsonResponse(body);
      }
    }

    if (url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/states$/)) {
      if (method === "GET") return jsonResponse(states);
      if (method === "POST") {
        const state = body;
        states = [...states, state];
        return jsonResponse(state, 201);
      }
    }
    const stateCaptureMatch = url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/states\/([^/]+)\/capture$/);
    if (stateCaptureMatch && method === "POST") {
      if (options.stateCaptureResponse) {
        return jsonResponse(options.stateCaptureResponse.body, options.stateCaptureResponse.status);
      }
      const stateID = decodeURIComponent(stateCaptureMatch[1]);
      states = states.map((state) => state.id === stateID ? { ...state, count: 4, timings_us: [9000, 4500, 560, 560] } : state);
      return jsonResponse({ ok: true, state: states.find((state) => state.id === stateID), required_samples: 1, valid_captures: 1 });
    }
    const stateMatch = url.match(/^\/api\/controller\/rooms\/[^/]+\/devices\/[^/]+\/ir\/states\/([^/]+)$/);
    if (stateMatch) {
      const stateID = decodeURIComponent(stateMatch[1]);
      if (method === "PUT") {
        const updatedState = { ...states.find((state) => state.id === stateID), ...body, id: stateID };
        states = states.map((state) => state.id === stateID ? updatedState : state);
        return jsonResponse(updatedState);
      }
      if (method === "DELETE") {
        states = states.filter((state) => state.id !== stateID);
        return jsonResponse({ ok: true, state_id: stateID, trash_entry_id: "trash_state_1", operation_id: "delete_state_1", status: "verified" });
      }
    }

    if (url === "/api/controller/rooms" && method === "GET") return jsonResponse(rooms);
    if (url === "/api/controller/rooms" && method === "POST") {
      if (failNextMutation) {
        failNextMutation = false;
        return jsonResponse({ error: "fixture mutation denied" }, 409);
      }
      const room = body as unknown as RoomFixture;
      rooms = [...rooms, room];
      devices[room.id] = [];
      if (holdNextMutation) {
        holdNextMutation = false;
        return new Promise<Response>((resolve) => { releaseHeldMutation = resolve; });
      }
      return jsonResponse(room, 201);
    }
    const roomMatch = url.match(/^\/api\/controller\/rooms\/([^/]+)$/);
    if (roomMatch && method === "PUT") {
      const roomId = decodeURIComponent(roomMatch[1]);
      const room = body as unknown as RoomFixture;
      rooms = rooms.map((current) => current.id === roomId ? room : current);
      return jsonResponse(room);
    }
    if (roomMatch && method === "DELETE") {
      const roomId = decodeURIComponent(roomMatch[1]);
      if ((devices[roomId] ?? []).length > 0) return jsonResponse({ error: "room still has devices" }, 409);
      rooms = rooms.filter((room) => room.id !== roomId);
      return jsonResponse({ ok: true, room_id: roomId, trash_entry_id: "trash_room_1", operation_id: "delete_room_1", status: "verified" });
    }

    const devicesMatch = url.match(/^\/api\/controller\/rooms\/([^/]+)\/devices$/);
    if (devicesMatch && method === "GET") {
      return jsonResponse(devices[decodeURIComponent(devicesMatch[1])] ?? []);
    }
    if (devicesMatch && method === "POST") {
      const roomId = decodeURIComponent(devicesMatch[1]);
      const device = body as unknown as DeviceFixture;
      devices[roomId] = [...(devices[roomId] ?? []), device];
      return jsonResponse(device, 201);
    }
    const deviceMatch = url.match(/^\/api\/controller\/rooms\/([^/]+)\/devices\/([^/]+)$/);
    if (deviceMatch && method === "PUT") {
      const [, encodedRoomId, encodedDeviceId] = deviceMatch;
      const roomId = decodeURIComponent(encodedRoomId);
      const deviceId = decodeURIComponent(encodedDeviceId);
      const device = body as unknown as DeviceFixture;
      devices[roomId] = (devices[roomId] ?? []).map((current) => current.id === deviceId ? device : current);
      return jsonResponse(device);
    }
    if (deviceMatch && method === "DELETE") {
      const roomId = decodeURIComponent(deviceMatch[1]);
      const deviceId = decodeURIComponent(deviceMatch[2]);
      devices[roomId] = (devices[roomId] ?? []).filter((device) => device.id !== deviceId);
      return jsonResponse({ ok: true, room_id: roomId, device_id: deviceId, trash_entry_id: "trash_device_1", operation_id: "delete_device_1", status: "verified" });
    }
    return jsonResponse({ error: `unhandled fixture request: ${method} ${url}` }, 500);
  });

  return {
    fetchMock,
    failNextMutation() { failNextMutation = true; },
    holdNextMutation() { holdNextMutation = true; },
    releaseHeldMutation(response: Response) { releaseHeldMutation?.(response); },
  };
}

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>;
}

function renderSettings(initialEntry = "/settings?room=bedroom&device=roomlight") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationProbe />
      <Routes>
        <Route path="/settings" element={<RoomDeviceSettingsPage />} />
        <Route path="/rooms/:roomId" element={<p>Home target</p>} />
        <Route path="/" element={<p>Home target</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Step 17H Room / Device settings route", () => {
  it("restores the selected Room and Device from settings URL and returns to that Home context", async () => {
    const fixture = createSettingsFixture();
    vi.stubGlobal("fetch", fixture.fetchMock);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=tv");

    expect(await screen.findByRole("button", { name: "TV (tv) を選択", pressed: true }, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByText(/選択中: Bedroom \(bedroom\) \/ TV \(tv\)/)).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "選択中のRoom / Deviceへ戻る" }));
    expect(screen.getByText("Home target")).toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/rooms/bedroom?device=tv");
  });

  it("changes the settings target when another Room is selected and returns to that Room", async () => {
    const fixture = createSettingsFixture();
    vi.stubGlobal("fetch", fixture.fetchMock);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=tv");

    await user.click(await screen.findByRole("button", { name: "Living" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Living" })).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByTestId("location")).toHaveTextContent("/settings?room=living");
    });
    expect(await screen.findByText(/このRoomにはDeviceがありません/)).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "選択中のRoom / Deviceへ戻る" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/rooms/living");
  });

  it("supports Room and Device create/edit/delete with validation and preserves delete impact guidance", async () => {
    const fixture = createSettingsFixture();
    vi.stubGlobal("fetch", fixture.fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const roomCreateForm = screen.getByRole("button", { name: "Room作成" }).closest("form");
    expect(roomCreateForm).not.toBeNull();
    await user.click(within(roomCreateForm as HTMLElement).getByRole("button", { name: "Room作成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Room IDと表示名を入力してください");

    await user.type(screen.getByLabelText("Room ID"), "studio");
    await user.type(screen.getByLabelText("表示名", { selector: "#room-create-name" }), "Studio");
    await user.type(screen.getByLabelText("表示優先度", { selector: "#room-create-sort-order" }), "not-a-number");
    await user.click(within(roomCreateForm as HTMLElement).getByRole("button", { name: "Room作成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("表示優先度には数値を入力してください");

    await user.clear(screen.getByLabelText("表示優先度", { selector: "#room-create-sort-order" }));
    await user.click(within(roomCreateForm as HTMLElement).getByRole("button", { name: "Room作成" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Studio" })).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByTestId("location")).toHaveTextContent("/settings?room=studio");
    });

    await user.clear(screen.getByLabelText("表示名", { selector: "#room-edit-name" }));
    await user.type(screen.getByLabelText("表示名", { selector: "#room-edit-name" }), "Studio East");
    await user.click(screen.getByRole("button", { name: "Room更新" }));
    expect(await screen.findByText("Roomを更新しました: Studio East")).toBeInTheDocument();

    const deviceCreateForm = screen.getByRole("button", { name: "Device作成" }).closest("form");
    expect(deviceCreateForm).not.toBeNull();
    await user.click(within(deviceCreateForm as HTMLElement).getByRole("button", { name: "Device作成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Device IDと表示名を入力してください");
    await user.type(screen.getByLabelText("Device ID"), "amp");
    await user.type(screen.getByLabelText("表示名", { selector: "#device-create-name" }), "Audio");
    await user.click(within(deviceCreateForm as HTMLElement).getByRole("button", { name: "Device作成" }));
    expect(await screen.findByRole("button", { name: "Audio (amp) を選択", pressed: true })).toBeInTheDocument();

    await user.clear(screen.getByLabelText("表示名", { selector: "#device-edit-name" }));
    await user.type(screen.getByLabelText("表示名", { selector: "#device-edit-name" }), "Studio Audio");
    await user.click(screen.getByRole("button", { name: "Device更新" }));
    expect(await screen.findByText("Deviceを更新しました: Studio Audio")).toBeInTheDocument();

    expect(screen.getByText(/adapters\/などの配下dataは再帰削除しません/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Device削除" }));
    expect(await screen.findByText(/Device metadataをTrashへ移動しました: Studio Audio.*trash_device_1.*delete_device_1.*verified/)).toBeInTheDocument();
    expect(await screen.findByText(/このRoomにはDeviceがありません/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Room削除" }));
    expect(await screen.findByText(/Room metadataをTrashへ移動しました: Studio East.*trash_room_1.*delete_room_1.*verified/)).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Bedroom" })).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByTestId("location")).toHaveTextContent("/settings?room=bedroom");
    });

    const requests = fixture.fetchMock.mock.calls.map(([input, init]) => `${init?.method ?? "GET"} ${String(input)}`);
    expect(requests).toContain("POST /api/controller/rooms");
    expect(requests).toContain("PUT /api/controller/rooms/studio");
    expect(requests).toContain("DELETE /api/controller/rooms/studio");
    expect(requests).toContain("POST /api/controller/rooms/studio/devices");
    expect(requests).toContain("PUT /api/controller/rooms/studio/devices/amp");
    expect(requests).toContain("DELETE /api/controller/rooms/studio/devices/amp");
  }, 15000);

  it("shows API errors and keeps mutation controls in a loading state until the response arrives", async () => {
    const fixture = createSettingsFixture();
    vi.stubGlobal("fetch", fixture.fetchMock);
    const user = userEvent.setup();
    renderSettings("/settings");
    await screen.findByRole("button", { name: "Bedroom" });

    fixture.failNextMutation();
    await user.type(screen.getByLabelText("Room ID"), "studio");
    await user.type(screen.getByLabelText("表示名", { selector: "#room-create-name" }), "Studio");
    await user.click(screen.getByRole("button", { name: "Room作成" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("fixture mutation denied"));

    fixture.holdNextMutation();
    await user.click(screen.getByRole("button", { name: "Room作成" }));
    expect(await screen.findByText("Room作成中: Studio")).toHaveAttribute("role", "status");
    expect(screen.getByRole("button", { name: "Room作成" })).toBeDisabled();
    fixture.releaseHeldMutation(jsonResponse({ id: "studio", name: "Studio" }, 201));
    await waitFor(() => expect(screen.getByRole("button", { name: "Studio" })).toBeInTheDocument());
  });
});

describe("Step 17I IR Adapter / State Schema settings", () => {
  it("shows command-type Adapter settings on Settings, asks before remote-type changes, and saves through the existing API", async () => {
    const fixture = createSettingsFixture();
    vi.stubGlobal("fetch", fixture.fetchMock);
    const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValue(true);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const configHeading = await screen.findByRole("heading", { name: "IR Adapter設定" });
    const configSection = configHeading.closest("section") as HTMLElement;
    expect(await within(configSection).findByText("IR Adapter設定を取得しました")).toBeInTheDocument();
    expect(within(configSection).getByLabelText("リモコン種別 (ir_remote_type)")).toHaveValue("command");
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();

    await user.selectOptions(within(configSection).getByLabelText("リモコン種別 (ir_remote_type)"), "stateful");
    await user.click(within(configSection).getByRole("button", { name: "保存" }));
    expect(await within(configSection).findByText("IRリモコン種別の変更をキャンセルしました")).toBeInTheDocument();
    expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/ir/config") && init?.method === "PUT")).toBe(false);

    await user.click(within(configSection).getByRole("button", { name: "保存" }));
    await waitFor(() => expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/ir/config") && init?.method === "PUT")).toBe(true));
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(confirm).toHaveBeenNthCalledWith(1, expect.stringContaining("IRリモコン種別を変更"));
    expect(await within(configSection).findByText("IR Adapter設定を保存しました")).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "リモコンの項目設定（Schema）" })).toBeInTheDocument();
  });

  it("allows an absent Adapter config and keeps its initial values editable", async () => {
    const fixture = createSettingsFixture({ missingIRConfig: true });
    vi.stubGlobal("fetch", fixture.fetchMock);
    renderSettings("/settings?room=bedroom&device=roomlight");

    expect(await screen.findByText(/IR Adapter設定は未作成です/)).toBeInTheDocument();
    expect(screen.getByLabelText("搬送波周波数 kHz (carrier_khz)")).toHaveValue(38);
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
  });

  it("guides State Schema creation when its read returns not found", async () => {
    const fixture = createSettingsFixture({ irRemoteType: "stateful" });
    vi.stubGlobal("fetch", fixture.fetchMock);
    renderSettings("/settings?room=bedroom&device=roomlight");

    expect(await screen.findByText(/State Schemaはまだ作成されていません/)).toBeInTheDocument();
    expect(screen.getByText(/既存Stateはありません/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "項目追加" })).toBeInTheDocument();
  });

  it("validates State Schema fields, preserves overwrite confirmation, and reports a save conflict", async () => {
    const fixture = createSettingsFixture({
      irRemoteType: "stateful",
      stateSchema: { power: { type: "enum", values: ["on", "off"] } },
      schemaUpdateStatus: 409,
    });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const alert = vi.fn();
    const confirm = vi.fn(() => true);
    vi.stubGlobal("alert", alert);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const schemaSection = (await screen.findByRole("heading", { name: "リモコンの項目設定（Schema）" }, { timeout: 5000 })).closest("section") as HTMLElement;
    await user.click(within(schemaSection).getByRole("button", { name: "項目追加" }));
    await user.click(within(schemaSection).getByRole("button", { name: "項目設定を保存" }));
    expect(await within(schemaSection).findByText("field名を入力してください")).toBeInTheDocument();
    expect(alert).toHaveBeenCalled();
    expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/ir/state-schema") && init?.method === "PUT")).toBe(false);

    await user.type(within(schemaSection).getByLabelText("項目ID", { selector: "#state-schema-key-1" }), "mode");
    await user.click(within(schemaSection).getByRole("button", { name: "項目設定を保存" }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("既存のIR State Schemaを上書き"));
    expect(await within(schemaSection).findByRole("alert")).toHaveTextContent("Schema conflicts with existing State");
    expect(within(schemaSection).getByText(/cool_26/)).toBeInTheDocument();
  });

  it("keeps config and Schema read errors distinct from missing settings and exposes retry controls", async () => {
    const fixture = createSettingsFixture({ irRemoteType: "stateful", failIRConfigRead: true });
    vi.stubGlobal("fetch", fixture.fetchMock);
    renderSettings("/settings?room=bedroom&device=roomlight");

    const configSection = (await screen.findByRole("heading", { name: "IR Adapter設定" })).closest("section") as HTMLElement;
    expect(await within(configSection).findByRole("alert")).toHaveTextContent("IR設定 fixture unavailable");
    expect(within(configSection).getByRole("button", { name: "保存" })).toBeDisabled();
    expect(screen.queryByRole("heading", { name: "リモコンの項目設定（Schema）" })).not.toBeInTheDocument();
  });

  it("reports an Adapter save conflict without claiming that settings were saved", async () => {
    const fixture = createSettingsFixture({ configUpdateStatus: 409 });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const configSection = (await screen.findByRole("heading", { name: "IR Adapter設定" })).closest("section") as HTMLElement;
    await within(configSection).findByText("IR Adapter設定を取得しました");
    await user.clear(within(configSection).getByLabelText("搬送波周波数 kHz (carrier_khz)"));
    await user.type(within(configSection).getByLabelText("搬送波周波数 kHz (carrier_khz)"), "40");
    await user.click(within(configSection).getByRole("button", { name: "保存" }));

    expect(await within(configSection).findByRole("alert")).toHaveTextContent("IR config conflict");
    expect(within(configSection).queryByText("IR Adapter設定を保存しました")).not.toBeInTheDocument();
  });

  it("shows Schema read failure without claiming that the Schema is missing", async () => {
    const fixture = createSettingsFixture({ irRemoteType: "stateful", failIRSchemaRead: true });
    vi.stubGlobal("fetch", fixture.fetchMock);
    renderSettings("/settings?room=bedroom&device=roomlight");

    const schemaSection = (await screen.findByRole("heading", { name: "リモコンの項目設定（Schema）" })).closest("section") as HTMLElement;
    expect(await within(schemaSection).findByRole("alert")).toHaveTextContent("schema fixture unavailable");
    expect(within(schemaSection).queryByText(/State Schemaはまだ作成されていません/)).not.toBeInTheDocument();
    expect(within(schemaSection).getByRole("button", { name: "設定を再読込" })).toBeEnabled();
  });
});

describe("Step 4 Stateful daily operation settings", () => {
  it("creates a raw_samples AC State from every saved Schema field", async () => {
    const fixture = createSettingsFixture({
      bedroomDevices: [{ id: "bedroom_ac", name: "寝室エアコン", room_id: "bedroom", device_type: "air_conditioner", primary_adapter: "ir" }],
      irRemoteType: "stateful",
      stateSchemaForManagement: {
        power: { type: "enum", values: ["on", "off"] },
        mode: { type: "enum", values: ["cool", "heat"] },
        target_temperature: { type: "number", min: 16, max: 30 },
        fan: { type: "enum", values: ["auto", "low"] },
        swing: { type: "enum", values: ["auto", "fixed"] },
      },
    });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=bedroom_ac");
    const section = (await screen.findByRole("heading", { name: "IR State" })).closest("section") as HTMLElement;
    const stateId = await within(section).findByLabelText("IR State ID");
    await waitFor(() => expect(stateId).toBeEnabled());
    await user.type(stateId, "cool_26_auto");
    await user.type(within(section).getByLabelText("表示名", { selector: "#ir-state-create-name" }), "冷房26℃自動");
    for (const [key, value] of Object.entries({ power: "on", mode: "cool", fan: "auto", swing: "auto" })) {
      await user.selectOptions(within(section).getByLabelText(`${key} (enum)`, { selector: `#ir-state-create-${key}` }), value);
    }
    await user.type(within(section).getByLabelText("target_temperature (number)", { selector: "#ir-state-create-target_temperature" }), "26");
    await user.click(within(section).getByRole("button", { name: "IR State作成" }));
    expect(await within(section).findByText(/IR Stateを作成しました: 冷房26℃自動/)).toBeInTheDocument();
    const create = fixture.fetchMock.mock.calls.find(([url, init]) => String(url).endsWith("/ir/states") && init?.method === "POST");
    expect(JSON.parse(String(create?.[1]?.body))).toMatchObject({
      id: "cool_26_auto", format: "raw_samples",
      state: { power: "on", mode: "cool", target_temperature: 26, fan: "auto", swing: "auto" },
    });
  });

  it("cancels re-learning without capture and reports insufficient captures while retaining the learned selection", async () => {
    const fixture = createSettingsFixture({
      irRemoteType: "stateful",
      stateSchemaForManagement: { power: { type: "enum", values: ["on", "off"] } },
      states: [{ id: "power_off", name: "停止", state: { power: "off" }, format: "raw_samples", count: 10, timings_us: Array(10).fill(560), captures: [{ valid: true, count: 10, timings_us: Array(10).fill(560) }] }],
      stateCaptureResponse: { status: 422, body: { ok: false, error: "not enough valid captures", required_samples: 3, valid_captures: 0, captures: [], count_distribution: {} } },
    });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const confirm = vi.fn(() => false);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");
    const section = (await screen.findByRole("heading", { name: "IR State" })).closest("section") as HTMLElement;
    const relearn = await within(section).findByRole("button", { name: "再学習を開始" });
    await user.click(relearn);
    expect(await within(section).findByText("IR State再学習をキャンセルしました")).toBeInTheDocument();
    expect(fixture.fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/power_off/capture"))).toHaveLength(0);
    confirm.mockReturnValue(true);
    await user.click(relearn);
    const error = await within(section).findByRole("alert");
    expect(error).toHaveTextContent("valid captures=0/3");
    expect(error).toHaveTextContent("既存の学習結果は維持されています");
    expect(within(section).getByLabelText("format", { selector: "#ir-state-edit-format" })).toBeDisabled();
    expect(within(section).getByRole("button", { name: "再学習を開始" })).toBeEnabled();
    expect(within(section).queryByText(/IR State学習を完了しました/)).not.toBeInTheDocument();
    expect(fixture.fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/states/power_off/capture"))).toHaveLength(1);
  });
});

describe("Step 17J IR Command / State management", () => {
  it("keeps Command CRUD, all format fields, capture and Trash result on Settings", async () => {
    const fixture = createSettingsFixture({
      commands: [{ id: "old_power", name: "Old Power", format: "raw_samples", count: 0, captures: [], timings_us: [] }],
    });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const confirm = vi.fn(() => true);
    vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const section = (await screen.findByRole("heading", { name: "IR Command" })).closest("section") as HTMLElement;
    expect(await within(section).findByRole("heading", { name: "IR Command作成" })).toBeInTheDocument();
    const format = await within(section).findByLabelText("format", { selector: "#ir-command-create-format" });
    expect(within(format).getAllByRole("option").map((option) => (option as HTMLOptionElement).value)).toEqual([
      "raw_samples", "raw", "code", "preset_command",
    ]);
    await user.selectOptions(format, "raw");
    expect(within(section).getByLabelText("RAW timings (us)", { selector: "#ir-command-create-raw" })).toBeInTheDocument();
    await user.selectOptions(format, "code");
    expect(within(section).getByLabelText("protocol", { selector: "#ir-command-create-protocol" })).toBeInTheDocument();
    expect(within(section).getByLabelText("code", { selector: "#ir-command-create-code" })).toBeInTheDocument();
    await user.selectOptions(format, "preset_command");
    expect(within(section).getByLabelText("プリセットIR Command", { selector: "#ir-command-create-preset" })).toBeInTheDocument();
    await user.selectOptions(format, "raw_samples");
    expect(within(section).getAllByText(/学習dataは直接編集できません/)).toHaveLength(2);

    await user.click(within(section).getByRole("button", { name: "IR Command作成" }));
    expect(await within(section).findByRole("alert")).toHaveTextContent("IR Command IDと表示名を入力してください");
    await user.type(within(section).getByLabelText("IR Command ID"), "bedroom_on");
    await user.type(within(section).getByLabelText("表示名", { selector: "#ir-command-create-name" }), "Bedroom On");
    await user.selectOptions(within(section).getByLabelText("format", { selector: "#ir-command-create-format" }), "preset_command");
    await user.selectOptions(within(section).getByLabelText("プリセットIR Command", { selector: "#ir-command-create-preset" }), "LIGHT_ON_CH3");
    await user.click(within(section).getByRole("button", { name: "IR Command作成" }));
    expect(await within(section).findByText("IR Commandを作成しました: Bedroom On")).toBeInTheDocument();
    const commandCreateCall = fixture.fetchMock.mock.calls.find(([url, init]) => String(url).endsWith("/ir/commands") && init?.method === "POST");
    expect(commandCreateCall).toBeDefined();
    expect(JSON.parse(String(commandCreateCall?.[1]?.body))).toMatchObject({ id: "bedroom_on", name: "Bedroom On" });

    await user.clear(within(section).getByLabelText("表示名", { selector: "#ir-command-edit-name" }));
    await user.type(within(section).getByLabelText("表示名", { selector: "#ir-command-edit-name" }), "Bedroom Main On");
    await user.click(within(section).getByRole("button", { name: "IR Command更新" }));
    expect(await within(section).findByText("IR Commandを更新しました: Bedroom Main On")).toBeInTheDocument();
    await user.click(within(section).getByRole("button", { name: "IR Command削除" }));
    expect(await within(section).findByText(/IR CommandをTrashへ移動しました: Bedroom Main On.*trash_command_1.*delete_command_1.*verified/)).toBeInTheDocument();
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("IR Command「Bedroom Main On」"));
    expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/ir/commands/bedroom_on") && init?.method === "DELETE")).toBe(true);
  }, 15000);

  it("uses Schema values for State CRUD, restricts capture to raw_samples, and shows Trash identifiers", async () => {
    const fixture = createSettingsFixture({
      irRemoteType: "stateful",
      stateSchemaForManagement: { power: { type: "enum", values: ["on", "off"] } },
      states: [{ id: "power_on", name: "Power On", state: { power: "on" }, format: "preset_command", pico_command: "AC_POWER_ON" }],
    });
    vi.stubGlobal("fetch", fixture.fetchMock);
    vi.stubGlobal("confirm", vi.fn(() => true));
    const user = userEvent.setup();
    renderSettings("/settings?room=bedroom&device=roomlight");

    const section = (await screen.findByRole("heading", { name: "IR State" })).closest("section") as HTMLElement;
    const format = await within(section).findByLabelText("format", { selector: "#ir-state-create-format" });
    expect(within(format).getAllByRole("option").map((option) => (option as HTMLOptionElement).value)).toEqual([
      "raw_samples", "raw", "code", "preset_command",
    ]);
    expect(within(section).getByLabelText("power (enum)", { selector: "#ir-state-create-power" })).toHaveValue("");
    expect(within(section).queryByRole("button", { name: "学習を開始" })).not.toBeInTheDocument();

    await user.selectOptions(format, "raw");
    expect(within(section).getByLabelText("RAW timings (us)", { selector: "#ir-state-create-raw" })).toBeInTheDocument();
    await user.selectOptions(format, "code");
    expect(within(section).getByLabelText("protocol", { selector: "#ir-state-create-protocol" })).toBeInTheDocument();
    await user.selectOptions(format, "preset_command");
    expect(within(section).getByLabelText("プリセットIR Command", { selector: "#ir-state-create-preset" })).toBeInTheDocument();
    await user.selectOptions(format, "raw_samples");

    await user.type(within(section).getByLabelText("IR State ID"), "power_off");
    await user.type(within(section).getByLabelText("表示名", { selector: "#ir-state-create-name" }), "Power Off");
    await user.selectOptions(within(section).getByLabelText("power (enum)", { selector: "#ir-state-create-power" }), "off");
    await user.click(within(section).getByRole("button", { name: "IR State作成" }));
    expect(await within(section).findByText(/IR Stateを作成しました: Power Off/)).toBeInTheDocument();
    const stateCreateCall = fixture.fetchMock.mock.calls.find(([url, init]) => String(url).endsWith("/ir/states") && init?.method === "POST");
    expect(stateCreateCall).toBeDefined();
    expect(JSON.parse(String(stateCreateCall?.[1]?.body))).toMatchObject({ id: "power_off", name: "Power Off" });

    expect(within(section).getByRole("button", { name: "学習を開始" })).toBeInTheDocument();
    await user.click(within(section).getByRole("button", { name: "学習を開始" }));
    expect(await within(section).findByText(/IR State学習を完了しました/)).toBeInTheDocument();
    expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/states/power_off/capture") && init?.method === "POST")).toBe(true);

    await user.clear(within(section).getByLabelText("表示名", { selector: "#ir-state-edit-name" }));
    await user.type(within(section).getByLabelText("表示名", { selector: "#ir-state-edit-name" }), "Power Off Learned");
    await user.click(within(section).getByRole("button", { name: "IR State更新" }));
    expect(await within(section).findByText(/IR Stateを更新しました: Power Off Learned/)).toBeInTheDocument();
    await user.click(within(section).getByRole("button", { name: "IR State削除" }));
    expect(await within(section).findByText(/IR StateをTrashへ移動しました: Power Off Learned.*trash_state_1.*delete_state_1.*verified/)).toBeInTheDocument();
    expect(fixture.fetchMock.mock.calls.some(([url, init]) => String(url).endsWith("/ir/states/power_off") && init?.method === "DELETE")).toBe(true);
  }, 15000);
});

describe("Step 4D Schema editor", () => {
  async function setup(schema: IRStateSchema, extras: Partial<SettingsFixtureOptions> = {}) {
    const fixture = createSettingsFixture({ irRemoteType: "stateful", stateSchema: schema, ...extras });
    vi.stubGlobal("fetch", fixture.fetchMock);
    const confirm = vi.fn(() => true); vi.stubGlobal("confirm", confirm); vi.stubGlobal("alert", vi.fn());
    const user = userEvent.setup(); renderSettings("/settings?room=bedroom&device=roomlight");
    const section = (await screen.findByRole("heading", { name: "リモコンの項目設定（Schema）" })).closest("section")!;
    const ui = within(section);
    await waitFor(() => expect(ui.getByRole("button", { name: "項目設定を保存" })).toBeEnabled());
    const puts = () => fixture.fetchMock.mock.calls.filter(([url, init]) => String(url).endsWith("/ir/state-schema") && init?.method === "PUT");
    const save = async () => { await user.click(ui.getByRole("button", { name: "項目設定を保存" })); await ui.findByText("IR State Schemaを保存しました"); };
    return { fixture, confirm, user, ui, puts, save };
  }
  it("explains basic configuration and persists a multiline remote note without changing input rules", async () => {
    const schema: IRStateSchema = { temperature: { type: "number", min: 14, max: 30 } };
    const { user, ui, puts, save } = await setup(schema);
    expect(ui.queryByText(/旧契約/)).not.toBeInTheDocument();
    expect(ui.getByText(/まず基本設定を合わせ/)).toBeInTheDocument();
    expect(ui.queryByLabelText("field 1 base presence")).not.toBeInTheDocument();
    await user.click(ui.getByText("表示名・説明・単位（任意）"));
    await user.click(ui.getByLabelText("field 1 base labelを指定"));
    await user.type(ui.getByLabelText("field 1 base label"), "設定温度");
    await user.click(ui.getByLabelText("field 1 base descriptionを指定"));
    const note = "リモコンの温度表示を確認\n送風では温度を使わない（取扱説明書で確認）";
    await user.type(ui.getByLabelText("field 1 base description"), note);
    await user.click(ui.getByLabelText("field 1 base unitを指定"));
    await user.type(ui.getByLabelText("field 1 base unit"), "℃");
    await save();
    expect(JSON.parse(String(puts()[0][1]?.body))).toEqual({ temperature: { ...schema.temperature, label: "設定温度", description: note, unit: "℃" } });
    await user.click(ui.getByRole("button", { name: "設定を再読込" }));
    await waitFor(() => expect(ui.getByLabelText("field 1 base description")).toHaveValue(note));
    expect(ui.getByLabelText("field 1 base label")).toHaveValue("設定温度");
    expect(ui.getByLabelText("field 1 base unit")).toHaveValue("℃");
    expect(ui.queryByLabelText("field 1 base presence")).not.toBeInTheDocument();
  });
  it("keeps legacy attributes and exact empty/comma/space enum values through PUT and reload", async () => {
    const schema: IRStateSchema = { note: { type: "string", min: 0 }, n: { type: "number", values: ["unused"] }, choice: { type: "enum", values: ["", "a,b", " spaced "] } };
    const { user, ui, puts, save } = await setup(schema);
    await save(); expect(JSON.parse(String(puts()[0][1]?.body))).toEqual(schema);
    await user.click(ui.getByRole("button", { name: "設定を再読込" }));
    await waitFor(() => expect(ui.getByDisplayValue("a,b")).toBeInTheDocument());
    await save(); expect(puts()).toHaveLength(2); expect(JSON.parse(String(puts()[1][1]?.body))).toEqual(schema);
  });
  it("keeps display-only edits legacy and asks before removing irrelevant constraints", async () => {
    const { user, ui, puts, save, confirm } = await setup({ note: { type: "string", min: 0 } });
    await user.click(ui.getByText("表示名・説明・単位（任意）")); await user.click(ui.getByLabelText("field 1 base labelを指定")); await user.type(ui.getByLabelText("field 1 base label"), "メモ");
    await save(); expect(JSON.parse(String(puts()[0][1]?.body))).toEqual({ note: { type: "string", min: 0, label: "メモ" } });
    confirm.mockReturnValueOnce(false); await user.click(ui.getByRole("button", { name: "入力ルールを追加" }));
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining("min=0")); expect(ui.queryByLabelText("field 1 base presence")).not.toBeInTheDocument();
    await user.click(ui.getByRole("button", { name: "入力ルールを追加" })); await save();
    expect(JSON.parse(String(puts()[1][1]?.body))).toEqual({ note: { type: "string", presence: "required", label: "メモ" } });
  });
  it("edits typed boolean conditions and empty case enum; reload preserves exact attributes", async () => {
    const schema: IRStateSchema = { enabled: { type: "boolean" }, choice: { type: "enum", values: [""], presence: "optional", label: "選択", cases: [{ when: { all: [{ field: "enabled", in: [false] }] }, presence: "optional", values: ["a"], unit: "" }] } };
    const { user, ui, puts, save } = await setup(schema);
    await user.click(ui.getByLabelText("field 2 case 1 condition 1 boolean:true")); await user.click(ui.getByLabelText("field 2 case 1 condition 1 boolean:false"));
    await user.clear(ui.getByDisplayValue("a")); await save();
    const expected = structuredClone(schema); expected.choice.cases![0].when.all[0].in = [true]; expected.choice.cases![0].values = [""];
    expect(puts()).toHaveLength(1); expect(JSON.parse(String(puts()[0][1]?.body))).toEqual(expected);
    await user.click(ui.getByRole("button", { name: "設定を再読込" }));
    await waitFor(() => expect(ui.getByLabelText("field 2 case 1 condition 1 boolean:true")).toBeChecked());
    expect(ui.getByLabelText("field 2 case 1 unit")).toHaveValue("");
  });
  it("adds/removes cases and conditions and blocks incomplete conditions before PUT", async () => {
    const { user, ui, puts, save } = await setup({ enabled: { type: "boolean" }, level: { type: "number", presence: "optional", min: 0, max: 5 } });
    await user.click(ui.getByRole("button", { name: "条件別の設定を追加" })); await user.click(ui.getByRole("button", { name: "項目設定を保存" }));
    expect(ui.getByText(/case 1: 条件を1つ以上追加/)).toBeInTheDocument(); expect(puts()).toHaveLength(0);
    await user.click(ui.getByRole("button", { name: "条件行を追加" })); await user.selectOptions(ui.getByLabelText("field 2 case 1 selector 1"), "enabled");
    await user.click(ui.getByLabelText("field 2 case 1 condition 1 boolean:false"));
    await user.type(ui.getByLabelText("field 2 case 1 min"), "-2"); await user.type(ui.getByLabelText("field 2 case 1 step"), "0.5"); await save();
    expect(JSON.parse(String(puts()[0][1]?.body)).level.cases).toEqual([{ when: { all: [{ field: "enabled", in: [false] }] }, presence: "required", min: -2, step: 0.5 }]);
    await user.click(ui.getByRole("button", { name: "条件行を削除" })); await user.click(ui.getByRole("button", { name: "条件別の設定を削除" })); await user.click(ui.getByRole("button", { name: "空の条件設定を解除" }));
    expect(ui.queryByRole("button", { name: "条件行を追加" })).not.toBeInTheDocument();
  });
  it.each([400, 409])("shows %i field/case/State issues and retains draft", async (status) => {
    const { user, ui, puts } = await setup({ power: { type: "enum", values: ["on", "off"] } }, { schemaUpdateStatus: status, schemaIssues: [{ field_id: "power", code: "enum", message: "fixture reason", case_index: 0, state_id: "cool_26" }] });
    await user.click(ui.getByRole("button", { name: "項目設定を保存" }));
    expect(await ui.findByRole("alert")).toHaveTextContent("power case 1 State cool_26: enum fixture reason");
    expect(ui.getByText("State cool_26: case 1: enum: fixture reason")).toBeInTheDocument(); expect(ui.getByLabelText("候補 1")).toHaveValue("on"); expect(puts()).toHaveLength(1);
  });
  it("shows changed display information and learned State before save, and cancellation makes no PUT", async () => {
    const { user, ui, puts, confirm } = await setup({ power: { type: "enum", values: ["on", "off"], label: "電源" } }, { states: [{ id: "learned", name: "登録済み", format: "raw_samples", state: { power: "on" }, count: 2, timings_us: [100, 200], captures: [] }] });
    await user.clear(ui.getByLabelText("field 1 base label")); await user.type(ui.getByLabelText("field 1 base label"), "変更電源");
    confirm.mockReturnValueOnce(false); await user.click(ui.getByRole("button", { name: "項目設定を保存" }));
    await ui.findByText("IR State Schemaの上書きをキャンセルしました");
    expect(confirm).toHaveBeenLastCalledWith(expect.stringMatching(/変更電源.*learned.*学習済み/)); expect(puts()).toHaveLength(0);
  });
  it("applies generic example only to draft and saves typed conditions without capture/send", async () => {
    const { user, ui, puts, fixture, save } = await setup({ power: { type: "enum", values: ["on", "off"] } });
    await user.selectOptions(ui.getByLabelText("標準テンプレート"), "conditional_example"); await user.click(ui.getByRole("button", { name: "テンプレートを適用" }));
    expect(ui.getByLabelText("項目ID", { selector: "#state-schema-key-0" })).toHaveValue("enabled"); expect(puts()).toHaveLength(0);
    await save(); const schema = JSON.parse(String(puts()[0][1]?.body));
    expect(schema.level.cases[0].when.all[0].in).toEqual([true]); expect(schema.note.allow_empty).toBe(false);
    expect(fixture.fetchMock.mock.calls.some(([url]) => /\/(send|capture)$/.test(String(url)))).toBe(false);
  });
  it("removes incompatible base and all case constraints using the confirmed exact proposal", async () => {
    const schema: IRStateSchema = { enabled: { type: "boolean" }, level: { type: "number", presence: "optional", min: 0, cases: [
      { when: { all: [{ field: "enabled", in: [true] }] }, presence: "required", min: -2, max: 2, step: 0.5, label: "強さ", unit: "" },
      { when: { all: [{ field: "enabled", in: [false] }] }, presence: "optional", max: 5, description: "停止時" },
    ] } };
    const { user, ui, confirm, puts, save } = await setup(schema);
    confirm.mockReturnValueOnce(false);
    await user.selectOptions(ui.getByLabelText("型", { selector: "#state-schema-type-1" }), "string");
    expect(ui.getByLabelText("型", { selector: "#state-schema-type-1" })).toHaveValue("number");
    expect(ui.getByLabelText("field 2 case 1 step")).toHaveValue(0.5);
    expect(puts()).toHaveLength(0);
    await user.selectOptions(ui.getByLabelText("型", { selector: "#state-schema-type-1" }), "string");
    const expected = structuredClone(schema);
    expected.level = { type: "string", presence: "optional", cases: [
      { when: { all: [{ field: "enabled", in: [true] }] }, presence: "required", label: "強さ", unit: "" },
      { when: { all: [{ field: "enabled", in: [false] }] }, presence: "optional", description: "停止時" },
    ] };
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining(`変更後: ${JSON.stringify(expected.level)}`));
    expect(confirm).toHaveBeenLastCalledWith(expect.stringMatching(/base.min=0.*case 1.min=-2.*case 1.max=2.*case 1.step=0.5.*case 2.max=5/));
    await save(); expect(puts()).toHaveLength(1);
    expect(JSON.parse(String(puts()[0][1]?.body))).toEqual(expected);
    await user.click(ui.getByRole("button", { name: "設定を再読込" }));
    await waitFor(() => expect(ui.getByLabelText("型", { selector: "#state-schema-type-1" })).toHaveValue("string"));
    expect(ui.getByLabelText("field 2 case 1 label")).toHaveValue("強さ");
  });
  it("keeps legacy presence omitted in both the type-change confirmation and PUT", async () => {
    const { user, ui, confirm, puts, save } = await setup({ note: { type: "number", min: 0 } });
    await user.selectOptions(ui.getByLabelText("型"), "string");
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining('変更後: {"type":"string"}'));
    expect(confirm).not.toHaveBeenLastCalledWith(expect.stringContaining('"presence"'));
    await save(); expect(JSON.parse(String(puts()[0][1]?.body))).toEqual({ note: { type: "string" } });
  });

});

describe("Step 4E conditional State input", () => {
  async function setup(schema: IRStateSchema, states: Record<string, unknown>[] = []) {
    const fixture = createSettingsFixture({ irRemoteType: "stateful", stateSchemaForManagement: schema, states });
    vi.stubGlobal("fetch", fixture.fetchMock); const confirm = vi.fn(() => true); vi.stubGlobal("confirm", confirm);
    const user = userEvent.setup(); renderSettings("/settings?room=bedroom&device=roomlight");
    const section = (await screen.findByRole("heading", { name: "IR State" })).closest("section")!; const ui = within(section);
    await waitFor(() => expect(ui.getByRole("button", { name: "IR State作成" })).toBeEnabled());
    const create = async (id: string) => { await user.type(ui.getByLabelText("IR State ID"), id); await user.type(ui.getByLabelText("表示名", { selector: "#ir-state-create-name" }), id); await user.click(ui.getByRole("button", { name: "IR State作成" })); };
    const writes = () => fixture.fetchMock.mock.calls.filter(([url, init]) => /\/ir\/states(?:\/[^/]+)?$/.test(String(url)) && ["POST", "PUT"].includes(init?.method ?? ""));
    return { fixture, confirm, user, ui, create, writes };
  }
  const acSchema: IRStateSchema = { mode: { type: "enum", values: ["cool", "fan", "dry"] }, temperature: { type: "number", presence: "absent", label: "設定温度", cases: [{ when: { all: [{ field: "mode", in: ["cool"] }] }, presence: "required", min: 14, max: 30, unit: "℃" }] }, dry_adjustment: { type: "number", presence: "absent", cases: [{ when: { all: [{ field: "mode", in: ["dry"] }] }, presence: "required", min: -2, max: 2 }] } };
  it("confirms excluded hidden values, cancels without POST, restores drafts, and saves fan without temperature", async () => {
    const { user, ui, confirm, create, writes } = await setup(acSchema);
    expect(ui.getByText(/設定温度: selectorの入力待ち/)).toBeInTheDocument();
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "cool");
    await user.type(ui.getByLabelText("設定温度 (number)"), "26");
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "fan");
    expect(ui.queryByLabelText("設定温度 (number)")).not.toBeInTheDocument(); expect(ui.getByText(/保存時に除外するdraft値: temperature="26"/)).toBeInTheDocument();
    confirm.mockReturnValueOnce(false); await create("fan_state"); await ui.findByText("IR State作成をキャンセルしました"); expect(writes()).toHaveLength(0);
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "cool"); expect(ui.getByLabelText("設定温度 (number)")).toHaveValue(26);
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "fan"); await user.click(ui.getByRole("button", { name: "IR State作成" }));
    await ui.findByText("IR Stateを作成しました: fan_state"); expect(JSON.parse(String(writes()[0][1]?.body)).state).toEqual({ mode: "fan" });
  });
  it.each([-2, 0, 2])("saves dry correction %i without a temperature", async (value) => {
    const { user, ui, create, writes } = await setup(acSchema);
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "dry"); await user.type(ui.getByLabelText("dry_adjustment (number)"), String(value));
    await create("dry_state"); await ui.findByText("IR Stateを作成しました: dry_state"); expect(JSON.parse(String(writes()[0][1]?.body)).state).toEqual({ mode: "dry", dry_adjustment: value });
  });
  it("keeps empty optional State and explicit zero/false/empty string/enum separate", async () => {
    const schema: IRStateSchema = { n: { type: "number", presence: "optional" }, b: { type: "boolean", presence: "optional" }, s: { type: "string", presence: "optional" }, e: { type: "enum", presence: "optional", values: [""] } };
    const { user, ui, create, writes } = await setup(schema); await create("empty"); await ui.findByText("IR Stateを作成しました: empty"); expect(JSON.parse(String(writes()[0][1]?.body)).state).toEqual({});
    for (const key of ["n", "b", "s", "e"]) await user.click(ui.getByLabelText(`create ${key}を入力`));
    await user.type(ui.getByLabelText("n (number)", { selector: "#ir-state-create-n" }), "0"); await user.selectOptions(ui.getByLabelText("b (boolean)", { selector: "#ir-state-create-b" }), "false");
    await user.selectOptions(ui.getByLabelText("e (enum)", { selector: "#ir-state-create-e" }), "");
    await create("explicit"); await ui.findByText("IR Stateを作成しました: explicit"); expect(JSON.parse(String(writes()[1][1]?.body)).state).toEqual({ n: 0, b: false, s: "", e: "" });
  });
  it.each(["raw_samples", "raw", "code", "preset_command"])("protects registered %s values/signals but saves name and repeat only", async (format) => {
    const signal = { count: 2, timings_us: [100, 200], captures: [], protocol: "PANASONIC_40", code: "344A90BC2C", pico_command: "LIGHT_ON_CH3" };
    const { user, ui, writes } = await setup({ power: { type: "enum", values: ["on", "off"] } }, [{ id: "learned", name: "学習済み", state: { power: "on" }, format, ...signal }]);
    await waitFor(() => expect(ui.getByLabelText("power (enum)", { selector: "#ir-state-edit-power" })).toBeDisabled()); expect(ui.getByLabelText("format", { selector: "#ir-state-edit-format" })).toBeDisabled();
    await user.clear(ui.getByLabelText("表示名", { selector: "#ir-state-edit-name" })); await user.type(ui.getByLabelText("表示名", { selector: "#ir-state-edit-name" }), "改名");
    await user.clear(ui.getByLabelText("保存repeat（0は既定値）")); await user.type(ui.getByLabelText("保存repeat（0は既定値）"), "2"); await user.click(ui.getByRole("button", { name: "IR State更新" }));
    await ui.findByText("IR Stateを更新しました: 改名"); expect(JSON.parse(String(writes()[0][1]?.body))).toEqual({ name: "改名", repeat: 2 });
  });
  it.each([-3, 3])("rejects dry correction %i before POST", async (value) => {
    const { user, ui, create, writes } = await setup(acSchema);
    await user.selectOptions(ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" }), "dry");
    await user.type(ui.getByLabelText("dry_adjustment (number)"), String(value)); await create("invalid");
    expect(ui.getByText("dry_adjustment: range")).toBeInTheDocument(); expect(writes()).toHaveLength(0);
  });
  it("resolves non-AC boolean/enum selectors and saves zero/false with an optional field omitted", async () => {
    const schema: IRStateSchema = { enabled: { type: "boolean" }, profile: { type: "enum", values: ["normal", "boost"] }, level: { type: "number", presence: "absent", cases: [{ when: { all: [{ field: "enabled", in: [true] }, { field: "profile", in: ["normal"] }] }, presence: "required", min: 0, max: 5, step: 1 }] }, eco: { type: "boolean", presence: "optional" }, note: { type: "string", presence: "optional", allow_empty: false } };
    const { user, ui, create, writes } = await setup(schema);
    await user.selectOptions(ui.getByLabelText("enabled (boolean)", { selector: "#ir-state-create-enabled" }), "true");
    await user.selectOptions(ui.getByLabelText("profile (enum)", { selector: "#ir-state-create-profile" }), "normal");
    await user.type(ui.getByLabelText("level (number)", { selector: "#ir-state-create-level" }), "0");
    await user.click(ui.getByLabelText("create ecoを入力")); await user.selectOptions(ui.getByLabelText("eco (boolean)", { selector: "#ir-state-create-eco" }), "false");
    await create("non_ac"); await ui.findByText("IR Stateを作成しました: non_ac");
    expect(JSON.parse(String(writes()[0][1]?.body)).state).toEqual({ enabled: true, profile: "normal", level: 0, eco: false });
  });

  it("blocks capture of incompatible saved State and keeps the original learning information", async () => {
    const { fixture, user, ui } = await setup({ power: { type: "enum", values: ["on", "off"] } }, [{ id: "bad", name: "要確認", state: { power: "invalid" }, format: "raw_samples", count: 2, timings_us: [100, 200], captures: [] }]);
    const capture = await ui.findByRole("button", { name: "再学習を開始" }); expect(capture).toBeDisabled(); await user.click(capture);
    expect(ui.getAllByText(/保存State不整合: power: enum/).length).toBeGreaterThan(0);
    expect(fixture.fetchMock.mock.calls.some(([url]) => String(url).endsWith("/states/bad/capture"))).toBe(false);
    expect(ui.getByLabelText("power (enum)", { selector: "#ir-state-edit-power" })).toBeDisabled();
  });

  it("uses field IDs for empty base labels, empty case labels and selector waiting text", async () => {
    const schema: IRStateSchema = { mode: { type: "enum", values: ["normal", "special"], label: "" }, level: { type: "number", presence: "absent", label: "", cases: [{ when: { all: [{ field: "mode", in: ["special"] }] }, presence: "required", label: "" }] } };
    const { user, ui } = await setup(schema);
    expect(ui.getByText("level: selectorの入力待ちです")).toBeInTheDocument();
    const selector = ui.getByLabelText("mode (enum)", { selector: "#ir-state-create-mode" });
    await user.selectOptions(selector, "special");
    expect(ui.getByLabelText("level (number)", { selector: "#ir-state-create-level" })).toBeInTheDocument();
  });

});
