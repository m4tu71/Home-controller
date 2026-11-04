const fixtureDevicePath =
  "/api/controller/rooms/fixture-room/devices/fixture-stateful-ac";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function fixtureResponse(method: string, pathname: string, search: string): Response {
  if (method !== "GET") {
    return jsonResponse(
      {
        ok: false,
        error: "fixture_mutation_blocked",
        detail: "The Step 9B browser fixture blocks all writes and sends locally.",
      },
      405,
    );
  }

  if (pathname === "/api/controller/rooms") {
    return jsonResponse([{ id: "fixture-room", name: "Fixture Room", sort_order: 1 }]);
  }

  if (pathname === "/api/controller/rooms/fixture-room/devices") {
    return jsonResponse([
      {
        id: "fixture-stateful-ac",
        name: "Fixture Stateful AC",
        room_id: "fixture-room",
        device_type: "air_conditioner",
        primary_adapter: "ir",
        capabilities: ["send"],
      },
    ]);
  }

  if (pathname === `${fixtureDevicePath}/ir/config`) {
    return jsonResponse({ ir_remote_type: "stateful", carrier_khz: 38, default_repeat: 1 });
  }

  if (pathname === `${fixtureDevicePath}/ir/state-schema`) {
    return jsonResponse({ ok: false, error: "ir state schema not found" }, 404);
  }

  if (pathname === `${fixtureDevicePath}/ir/states`) {
    return jsonResponse([]);
  }

  if (pathname === "/api/controller/maintenance/data-health") {
    return jsonResponse({ ok: true, issues: [] });
  }

  if (pathname === "/api/controller/maintenance/operations") {
    return jsonResponse({ operations: [] });
  }

  if (pathname === "/api/controller/maintenance/trash") {
    const query = new URLSearchParams(search);
    return jsonResponse({
      entries: [],
      total: 0,
      offset: Number(query.get("offset") ?? 0),
      limit: Number(query.get("limit") ?? 25),
    });
  }

  return jsonResponse(
    {
      ok: false,
      error: "fixture_route_not_defined",
      detail: `No Step 9B fixture response is defined for GET ${pathname}. No request was sent to the Backend.`,
    },
    501,
  );
}

export function installStep9BStatefulEmptyFixture(): void {
  const realFetch = window.fetch.bind(window);

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const inputURL = input instanceof Request ? input.url : input.toString();
    const url = new URL(inputURL, window.location.href);

    if (!url.pathname.startsWith("/api/controller/")) {
      return realFetch(input, init);
    }

    const method = (init?.method ?? (input instanceof Request ? input.method : "GET")).toUpperCase();
    return fixtureResponse(method, url.pathname, url.search);
  };
}
