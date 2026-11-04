import { afterEach, describe, expect, it, vi } from "vitest";

import { createDevice, createRoom, updateDevice, updateRoom } from "./controller";

afterEach(() => vi.unstubAllGlobals());

describe("Room and Device client contract", () => {
  const room = { id: "room", name: "Room" };
  const device = {
    id: "device",
    name: "Device",
    room_id: "room",
    device_type: "light",
    primary_adapter: "ir",
  };

  it.each([
    { name: "Room", call: () => createRoom(room), path: "/api/controller/rooms", body: room },
    { name: "Device", call: () => createDevice("room", device), path: "/api/controller/rooms/room/devices", body: device },
  ])("accepts 201 with the created $name body", async ({ call, path, body }) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
      status: 201,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(call()).resolves.toEqual(body);
    expect(fetchMock).toHaveBeenCalledWith(path, expect.objectContaining({
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }));
  });

  it.each([
    { name: "Room", call: () => createRoom(room), error: "room already exists", message: "同じRoom IDがすでに存在します" },
    { name: "Device", call: () => createDevice("room", device), error: "device already exists", message: "同じDevice IDがすでに存在します" },
  ])("reports a 409 $name duplicate without treating it as success", async ({ call, error, message }) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: false, error }), {
      status: 409,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(call()).rejects.toThrow(message);
  });

  it.each([
    { name: "Room", call: () => updateRoom("room", room), body: room },
    { name: "Device", call: () => updateDevice("room", "device", device), body: device },
  ])("keeps 200 $name updates compatible", async ({ call, body }) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })));

    await expect(call()).resolves.toEqual(body);
  });
});
