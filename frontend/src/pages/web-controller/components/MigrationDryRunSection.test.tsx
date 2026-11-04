import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MigrationDryRunSection } from "./MigrationDryRunSection";

const plan = {
  version: 1,
  plan_id: "plan_migration_01",
  operation_type: "migration",
  payload_schema_id: "migration.v1",
  payload: {
    actions: [{
      action_id: "migrate_1",
      action: "write_ir_command",
      source: {
        path: "ir-signals/power.json",
        exists: true,
        kind: "file",
        size: 128,
        sha256: "a".repeat(64),
      },
      source_format: "raw",
      target_before: {
        path: "rooms/living/devices/tv/adapters/ir/commands/power.json",
        exists: false,
        size: 0,
      },
      planned_writes: [{
        path: "rooms/living/devices/tv/adapters/ir/commands/power.json",
        kind: "json",
        role: "operation",
        before: { path: "rooms/living/devices/tv/adapters/ir/commands/power.json", exists: false, size: 0 },
        expected_after_state: "created",
      }],
      target_format: "raw",
      transform: "preserve_raw",
      expected_after_state: "target_created",
      decision: { room_id: "living", device_id: "tv", target_id: "power" },
    }],
  },
  created_at: "2026-09-25T00:00:00Z",
  ready_to_apply: true,
  backup_requirement: "apply",
  actions: [{
    action_id: "migrate_1",
    action: "write_ir_command",
    selectable: true,
    destructive: false,
    backup_sources: ["rooms"],
  }],
  plan_digest: `sha256:${"b".repeat(64)}`,
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function installMigrationFetch(fetchMock = vi.fn()) {
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

async function fillValidMapping(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("移行元ファイル名（.jsonを除く）"), "power");
  await user.type(screen.getByLabelText("Room ID"), "living");
  await user.type(screen.getByLabelText("Device ID"), "tv");
  await user.type(screen.getByLabelText("Command ID"), "power");
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("MigrationDryRunSection", () => {
  it("submits explicit mapping decisions and displays the saved plan and source hash", async () => {
    const fetchMock = installMigrationFetch();
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true, plan }));
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await fillValidMapping(user);
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    expect(await screen.findByText("plan_migration_01")).toBeInTheDocument();
    expect(screen.getByText(`sha256:${"b".repeat(64)}`)).toBeInTheDocument();
    expect(screen.getAllByText("ir-signals/power.json")).toHaveLength(2);
    expect(screen.getByText("a".repeat(64))).toBeInTheDocument();
    expect(screen.getAllByText("rooms/living/devices/tv/adapters/ir/commands/power.json").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText(/apply準備完了/)).toBeInTheDocument();
    expect(screen.getByText(/applyとverifyは以下で別々に確認/)).toBeInTheDocument();
    expect(screen.getByText(/living \/ tv \/ power/)).toBeInTheDocument();

    const request = fetchMock.mock.calls.find((call) => call[0] === "/api/controller/maintenance/migration/dry-run");
    expect(request?.[1]).toMatchObject({
      method: "POST",
      headers: { "Content-Type": "application/json" },
      cache: "no-store",
    });
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      mappings: [{
        action_id: "migrate_1",
        source_path: "ir-signals/power.json",
        target_kind: "command",
        room_id: "living",
        device_id: "tv",
        target_id: "power",
        decision: { room_id: "living", device_id: "tv", target_id: "power" },
      }],
    });
  });

  it("reports a 409 source/target conflict or stale observation without showing a plan", async () => {
    const fetchMock = installMigrationFetch();
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: "migration target exists or source changed" }, 409));
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await fillValidMapping(user);
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("競合・変更があります");
    expect(screen.getByRole("alert")).toHaveTextContent("migration target exists or source changed");
    expect(screen.queryByText("保存されたdry-run plan")).not.toBeInTheDocument();
  });

  it("shows invalid mapping or source data errors returned by the API", async () => {
    const fetchMock = installMigrationFetch();
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: "invalid migration mapping or source" }, 400));
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await fillValidMapping(user);
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("入力または移行元データを確認してください");
    expect(screen.getByRole("alert")).toHaveTextContent("invalid migration mapping or source");
  });

  it("does not request the retired source list and rejects unsafe typed filenames", async () => {
    const fetchMock = installMigrationFetch();
    const user = userEvent.setup();
    render(<MigrationDryRunSection />);
    expect(fetchMock).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("移行元ファイル名（.jsonを除く）"), "../unsafe");
    await user.type(screen.getByLabelText("Room ID"), "living");
    await user.type(screen.getByLabelText("Device ID"), "tv");
    await user.type(screen.getByLabelText("Command ID"), "power");
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("移行元ファイル名には英数字");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps an undecided State payload visible as a non-ready plan", async () => {
    const pendingPlan = {
      ...plan,
      ready_to_apply: false,
      actions: [{ ...plan.actions[0], selectable: false }],
      payload: {
        actions: [{
          ...plan.payload.actions[0],
          target_before: {
            ...plan.payload.actions[0].target_before,
            path: "rooms/living/devices/tv/adapters/ir/states/power_on.json",
          },
          decision: { room_id: "living", device_id: "tv", target_id: "power_on" },
        }],
      },
    };
    const fetchMock = installMigrationFetch();
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true, plan: pendingPlan }));
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await user.type(screen.getByLabelText("移行元ファイル名（.jsonを除く）"), "power");
    await user.selectOptions(screen.getByLabelText("移行先種別"), "state");
    await user.type(screen.getByLabelText("Room ID"), "living");
    await user.type(screen.getByLabelText("Device ID"), "tv");
    await user.type(screen.getByLabelText("State ID"), "power_on");
    expect(screen.getByPlaceholderText('{"power":"on"}')).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    expect(await screen.findByText("人の判断または追加情報が必要")).toBeInTheDocument();
    expect(screen.getByText("選択不可")).toBeInTheDocument();
    expect(screen.getByText(/State payload: 未決定/)).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  });

  it("submits optional typed Room, Device, IR config and State schema scaffolds", async () => {
    const statePath = "rooms/living/devices/tv/adapters/ir/states/power_on.json";
    const scaffoldPlan = {
      ...plan,
      payload: {
        actions: [{
          ...plan.payload.actions[0],
          target_before: { ...plan.payload.actions[0].target_before, path: statePath },
          planned_writes: [
            {
              path: "rooms/living/room.json",
              kind: "json",
              role: "room",
              before: { path: "rooms/living/room.json", exists: false, size: 0 },
              expected_after_state: "created",
              content: { id: "living", name: "Living Room" },
            },
            {
              path: "rooms/living/devices/tv/device.json",
              kind: "json",
              role: "device",
              before: { path: "rooms/living/devices/tv/device.json", exists: false, size: 0 },
              expected_after_state: "created",
              content: { id: "tv", room_id: "living", name: "Television", device_type: "tv", primary_adapter: "ir" },
            },
            {
              path: "rooms/living/devices/tv/adapters/ir/ir_config.json",
              kind: "json",
              role: "ir_config",
              before: { path: "rooms/living/devices/tv/adapters/ir/ir_config.json", exists: false, size: 0 },
              expected_after_state: "created",
              content: { ir_remote_type: "stateful", carrier_khz: 38, default_repeat: 2 },
            },
            {
              path: "rooms/living/devices/tv/adapters/ir/state_schema.json",
              kind: "json",
              role: "state_schema",
              before: { path: "rooms/living/devices/tv/adapters/ir/state_schema.json", exists: false, size: 0 },
              expected_after_state: "created",
              content: { power: { type: "enum", values: ["off", "on"] } },
            },
            {
              path: statePath,
              kind: "json",
              role: "operation",
              before: { path: statePath, exists: false, size: 0 },
              expected_after_state: "created",
            },
          ],
          decision: {
            room_id: "living",
            device_id: "tv",
            target_id: "power_on",
            state: { power: "on" },
          },
        }],
      },
    };
    const fetchMock = installMigrationFetch();
    fetchMock.mockResolvedValueOnce(jsonResponse({ ok: true, plan: scaffoldPlan }));
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await user.type(screen.getByLabelText("移行元ファイル名（.jsonを除く）"), "power");
    await user.selectOptions(screen.getByLabelText("移行先種別"), "state");
    await user.type(screen.getByLabelText("Room ID"), "living");
    await user.type(screen.getByLabelText("Device ID"), "tv");
    await user.type(screen.getByLabelText("State ID"), "power_on");

    await user.click(screen.getByLabelText("Room metadataを新規作成"));
    await user.type(screen.getByLabelText("Room表示名"), "Living Room");
    await user.click(screen.getByLabelText("Device metadataを新規作成（primary_adapterはir）"));
    await user.type(screen.getByLabelText("Device表示名"), "Television");
    await user.type(screen.getByLabelText("device_type"), "tv");
    await user.type(screen.getByLabelText("capabilities（任意、カンマ区切り）"), "ir, hdmi");
    await user.click(screen.getByLabelText("IR configを新規作成"));
    await user.type(screen.getByLabelText("carrier_khz（移行元と一致必須）"), "38");
    await user.type(screen.getByLabelText("default_repeat（任意、0以上）"), "2");
    await user.click(screen.getByLabelText("State schemaを新規作成"));
    fireEvent.change(screen.getByPlaceholderText('{"power":{"type":"enum","values":["off","on"]}}'), {
      target: { value: '{"power":{"type":"enum","values":["off","on"]}}' },
    });
    fireEvent.change(screen.getByPlaceholderText('{"power":"on"}'), {
      target: { value: '{"power":"on"}' },
    });
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    expect(await screen.findByText(/Living Room/)).toBeInTheDocument();
    expect(screen.getAllByText(/state_schema/).length).toBeGreaterThanOrEqual(2);
    const request = fetchMock.mock.calls.find((call) => call[0] === "/api/controller/maintenance/migration/dry-run");
    const body = JSON.parse(String(request?.[1]?.body));
    expect(body.mappings[0]).toMatchObject({
      room: { id: "living", name: "Living Room" },
      device: {
        id: "tv",
        name: "Television",
        room_id: "living",
        device_type: "tv",
        primary_adapter: "ir",
        capabilities: ["ir", "hdmi"],
      },
      ir_config: { ir_remote_type: "stateful", carrier_khz: 38, default_repeat: 2 },
      state_schema: { power: { type: "enum", values: ["off", "on"] } },
      decision: {
        room_id: "living",
        device_id: "tv",
        target_id: "power_on",
        state: { power: "on" },
      },
    });
  });

  it("locks mappings during dry-run so the response cannot describe older inputs", async () => {
    let resolveDryRun: ((response: Response) => void) | undefined;
    const pendingDryRun = new Promise<Response>((resolve) => {
      resolveDryRun = resolve;
    });
    const fetchMock = installMigrationFetch();
    fetchMock.mockReturnValueOnce(pendingDryRun);
    const user = userEvent.setup();

    render(<MigrationDryRunSection />);
    await fillValidMapping(user);
    await user.click(screen.getByRole("button", { name: "dry-run planを作成" }));

    const roomInput = screen.getByLabelText("Room ID");
    const commandInput = screen.getByLabelText("Command ID");
    expect(roomInput).toBeDisabled();
    expect(commandInput).toBeDisabled();
    expect(screen.getByRole("button", { name: "Mappingを追加" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "plan作成中..." })).toBeDisabled();
    fireEvent.change(roomInput, { target: { value: "changed_while_pending" } });
    expect(roomInput).toHaveValue("living");

    await act(async () => {
      resolveDryRun?.(jsonResponse({ ok: true, plan }));
    });

    expect(await screen.findByText("plan_migration_01")).toBeInTheDocument();
    expect(roomInput).toHaveValue("living");
    expect(commandInput).toHaveValue("power");
  });
});
