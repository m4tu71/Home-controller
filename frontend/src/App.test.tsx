import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App";

vi.mock("./pages/web-controller/WebController", () => ({
  default: ({
    routeRoomId,
    routeDeviceId,
    onRoomSelect,
  }: {
    routeRoomId?: string;
    routeDeviceId?: string;
    onRoomSelect?: (roomId: string) => void;
  }) => (
    <main>
      <h1>Home Controller画面</h1>
      <p data-testid="route-room">{routeRoomId ?? "root"}</p>
      <p data-testid="route-device">{routeDeviceId ?? "none"}</p>
      <button type="button" onClick={() => onRoomSelect?.("living")}>Select Living</button>
    </main>
  ),
}));
vi.mock("./pages/web-controller/RoomDeviceSettingsPage", () => ({
  default: () => <main><h1>Room / Device設定画面</h1></main>,
}));
vi.mock("./pages/web-controller/MaintenancePage", () => ({
  default: () => <main><h1>Maintenance専用画面</h1></main>,
}));
vi.mock("./pages/web-controller/TrashPage", () => ({
  default: () => <main><h1>Trash専用画面</h1></main>,
}));
vi.mock("./components/footer/Footer", () => ({
  default: () => <footer>Version表示</footer>,
}));

function mockTime() {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
    date: "2026-09-27",
    time: "12:34:56",
    datetime: "2026-09-27 12:34:56",
  }), { status: 200, headers: { "Content-Type": "application/json" } })));
}

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState({}, "", "/");
});

describe("Step 17D Home route and Header", () => {
  it("renders the controller at / with the brand-name Home link and Header Time", async () => {
    mockTime();
    window.history.replaceState({}, "", "/");
    render(<App />);

    expect(screen.getByRole("heading", { name: "Home Controller画面" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Home Controller" })).toHaveAttribute("href", "/");
    expect(screen.queryByText("HC")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Home" })).toHaveAttribute("aria-current", "page");
    expect(await screen.findByText("12:34:56")).toBeInTheDocument();
    expect(screen.getByText("12:34:56").closest("time")).toHaveAttribute("datetime", "2026-09-27T12:34:56");
  });

  it("replaces the old URL with / and keeps browser navigation usable", async () => {
    mockTime();
    window.history.replaceState({}, "", "/web-controller");
    const user = userEvent.setup();
    render(<App />);

    await waitFor(() => expect(window.location.pathname).toBe("/") );
    expect(screen.getByRole("heading", { name: "Home Controller画面" })).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Maintenance" }));
    expect(window.location.pathname).toBe("/maintenance");
    expect(screen.getByRole("heading", { name: "Maintenance専用画面" })).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Home Controller" }));
    expect(window.location.pathname).toBe("/");
    expect(screen.getByRole("heading", { name: "Home Controller画面" })).toBeInTheDocument();

    window.history.back();
    await waitFor(() => expect(window.location.pathname).toBe("/maintenance"));
    expect(screen.getByRole("heading", { name: "Maintenance専用画面" })).toBeInTheDocument();
  });

  it.each([1280, 320])("keeps Header controls available at %ipx and supports keyboard Home navigation", async (width) => {
    mockTime();
    vi.stubGlobal("innerWidth", width);
    window.history.replaceState({}, "", "/raspy-time");
    const user = userEvent.setup();
    render(<App />);

    await waitFor(() => expect(window.location.pathname).toBe("/"));

    await user.tab();
    const homeLink = screen.getByRole("link", { name: "Home Controller" });
    expect(homeLink).toHaveFocus();
    expect(screen.queryByText("HC")).not.toBeInTheDocument();
    expect(await screen.findByText("12:34:56")).toBeInTheDocument();
    await user.keyboard("{Enter}");
    expect(window.location.pathname).toBe("/");
    expect(screen.getByRole("heading", { name: "Home Controller画面" })).toBeInTheDocument();
  });

  it("redirects the retired Raspy Time URL without showing a navigation link", async () => {
    mockTime();
    window.history.replaceState({}, "", "/raspy-time");
    render(<App />);
    await waitFor(() => expect(window.location.pathname).toBe("/"));
    expect(screen.queryByRole("link", { name: "Raspy Time" })).not.toBeInTheDocument();
  });
});

describe("Step 17E Room routes", () => {
  it("lets the root page turn a selected Room into a bookmarkable URL", async () => {
    mockTime();
    window.history.replaceState({}, "", "/");
    const user = userEvent.setup();
    render(<App />);

    expect(screen.getByTestId("route-room")).toHaveTextContent("root");
    await user.click(screen.getByRole("button", { name: "Select Living" }));
    await waitFor(() => expect(window.location.pathname).toBe("/rooms/living"));
    expect(screen.getByTestId("route-room")).toHaveTextContent("living");
  });

  it("opens a direct Room URL and keeps Room selection in sync with back and forward", async () => {
    mockTime();
    window.history.replaceState({}, "", "/rooms/bedroom");
    const user = userEvent.setup();
    render(<App />);

    expect(screen.getByTestId("route-room")).toHaveTextContent("bedroom");
    await user.click(screen.getByRole("button", { name: "Select Living" }));
    await waitFor(() => expect(window.location.pathname).toBe("/rooms/living"));
    expect(screen.getByTestId("route-room")).toHaveTextContent("living");

    window.history.back();
    await waitFor(() => expect(window.location.pathname).toBe("/rooms/bedroom"));
    expect(screen.getByTestId("route-room")).toHaveTextContent("bedroom");

    window.history.forward();
    await waitFor(() => expect(window.location.pathname).toBe("/rooms/living"));
    expect(screen.getByTestId("route-room")).toHaveTextContent("living");
  });

  it("opens the dedicated Room / Device settings route", () => {
    mockTime();
    window.history.replaceState({}, "", "/settings?room=bedroom&device=roomlight");
    render(<App />);

    expect(screen.getByRole("heading", { name: "Room / Device設定画面" })).toBeInTheDocument();
  });

  it("passes a bookmarked Device selection through the Home route", () => {
    mockTime();
    window.history.replaceState({}, "", "/rooms/bedroom?device=roomlight");
    render(<App />);

    expect(screen.getByTestId("route-room")).toHaveTextContent("bedroom");
    expect(screen.getByTestId("route-device")).toHaveTextContent("roomlight");
  });
});

describe("Step 17K Maintenance and Trash routes", () => {
  it("opens dedicated routes from Home navigation and returns to daily operations or Settings", async () => {
    mockTime();
    window.history.replaceState({}, "", "/");
    const user = userEvent.setup();
    render(<App />);

    await user.click(screen.getByRole("link", { name: "Maintenance" }));
    expect(window.location.pathname).toBe("/maintenance");
    expect(screen.getByRole("heading", { name: "Maintenance専用画面" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Maintenance" })).toHaveAttribute("aria-current", "page");

    await user.click(screen.getByRole("link", { name: "Trash" }));
    expect(window.location.pathname).toBe("/trash");
    expect(screen.getByRole("heading", { name: "Trash専用画面" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Trash" })).toHaveAttribute("aria-current", "page");

    await user.click(screen.getByRole("link", { name: "Home Controller" }));
    expect(window.location.pathname).toBe("/");
    expect(screen.getByRole("heading", { name: "Home Controller画面" })).toBeInTheDocument();
  });
});
