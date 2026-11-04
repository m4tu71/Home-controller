type Delivery = "sent" | "not_sent" | "partially_sent" | "unknown";

const base = "/api/controller/rooms/fixture-room/devices/fixture-light";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

export function installStep17MSendResultFixture(delivery: Delivery): void {
  const realFetch = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const inputURL = input instanceof Request ? input.url : input.toString();
    const url = new URL(inputURL, window.location.href);
    if (!url.pathname.startsWith("/api/controller/")) return realFetch(input, init);

    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    if (method === "GET") {
      if (url.pathname === "/api/controller/rooms") return jsonResponse([{ id: "fixture-room", name: "Fixture Room" }]);
      if (url.pathname === "/api/controller/rooms/fixture-room/devices") {
        return jsonResponse([{ id: "fixture-light", name: "Fixture Light", room_id: "fixture-room", device_type: "light", primary_adapter: "ir" }]);
      }
      if (url.pathname === `${base}/ir/config`) return jsonResponse({ ir_remote_type: "command", carrier_khz: 38, default_repeat: 1 });
      if (url.pathname === `${base}/ir/commands`) {
        return jsonResponse([{ id: "fixture_on", name: "Fixture ON", format: "preset_command", pico_command: "LIGHT_ON_CH3" }]);
      }
    }

    if (method === "POST" && url.pathname === `${base}/ir/commands/fixture_on/send`) {
      if (delivery === "sent") {
        return jsonResponse({ ok: true, format: "preset_command", delivery_state: "sent", requested_repeats: 1, completed_repeats: 1, retryable_with: [] });
      }
      return jsonResponse({
        ok: false,
        error: "fixture send result",
        detail: "No IR signal was transmitted.",
        format: "preset_command",
        delivery_state: delivery,
        requested_repeats: 1,
        completed_repeats: 0,
        retryable_with: [],
      }, 502);
    }

    return jsonResponse({ ok: false, error: "fixture_route_blocked", detail: "No controller request was forwarded to the Backend." }, 405);
  };
}
