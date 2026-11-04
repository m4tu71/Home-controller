import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createIRCommand,
  createIRState,
  IRStateAPIError,
  type IRCommand,
  type IRState,
} from "./controller";

afterEach(() => vi.unstubAllGlobals());

describe("IR Adapter create API contract", () => {
  const command: IRCommand = {
    id: "power_on",
    name: "Power On",
    format: "preset_command",
    pico_command: "LIGHT_ON_CH3",
  };
  const state: IRState = {
    id: "power_on",
    name: "Power On",
    state: { power: "on" },
    format: "preset_command",
    pico_command: "LIGHT_ON_CH3",
  };

  it.each([
    { name: "Command", call: () => createIRCommand("bedroom", "roomlight", command), payload: command, collection: "commands" },
    { name: "State", call: () => createIRState("bedroom", "roomlight", state), payload: state, collection: "states" },
  ])("accepts 201 with the created $name body", async ({ call, payload, collection }) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), {
      status: 201,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(call()).resolves.toEqual(payload);
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/controller/rooms/bedroom/devices/roomlight/ir/${collection}`,
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      }),
    );
  });

  it("preserves the State API error status and payload for duplicate 409", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      ok: false,
      error: "ir state already exists",
    }), {
      status: 409,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(createIRState("bedroom", "roomlight", state)).rejects.toMatchObject({
      name: "IRStateAPIError",
      status: 409,
      payload: { ok: false, error: "ir state already exists" },
    } satisfies Partial<IRStateAPIError>);
  });
});
