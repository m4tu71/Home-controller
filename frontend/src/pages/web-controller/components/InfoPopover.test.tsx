import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { InfoPopover } from "./InfoPopover";

describe("InfoPopover", () => {
  it("opens on mouse hover and closes after the pointer leaves", async () => {
    const user = userEvent.setup();
    render(<InfoPopover subject="Room Bedroom" title="Bedroom のRoom情報"><p>Room ID: bedroom</p></InfoPopover>);

    const button = screen.getByRole("button", { name: "Room Bedroomの情報" });
    await user.hover(button);
    expect(screen.getByRole("region", { name: "Bedroom のRoom情報" })).toBeInTheDocument();
    expect(button).toHaveAttribute("aria-expanded", "true");
    await user.unhover(button);
    expect(screen.queryByRole("region", { name: "Bedroom のRoom情報" })).not.toBeInTheDocument();
    expect(button).toHaveAttribute("aria-expanded", "false");
  });

  it("opens from a touch pointer tap", async () => {
    const user = userEvent.setup();
    render(<InfoPopover subject="State Cool 26" title="Cool 26 のState情報"><p>mode=cool</p></InfoPopover>);

    const button = screen.getByRole("button", { name: "State Cool 26の情報" });
    await user.pointer([
      { keys: "[TouchA>]", target: button },
      { keys: "[/TouchA]", target: button },
    ]);
    expect(screen.getByRole("region", { name: "Cool 26 のState情報" })).toBeInTheDocument();
    expect(button).toHaveAttribute("aria-expanded", "true");
  });

  it("toggles by click, Enter, and Space; Escape closes and restores trigger focus", async () => {
    const user = userEvent.setup();
    render(<><button type="button">前へ</button><InfoPopover subject="Device Light" title="Light のDevice情報"><p>Device ID: light</p></InfoPopover></>);

    const button = screen.getByRole("button", { name: "Device Lightの情報" });
    await user.click(button);
    expect(screen.getByRole("region", { name: "Light のDevice情報" })).toBeInTheDocument();
    await user.click(button);
    expect(screen.queryByRole("region", { name: "Light のDevice情報" })).not.toBeInTheDocument();

    screen.getByRole("button", { name: "前へ" }).focus();
    await user.tab();
    expect(button).toHaveFocus();
    expect(screen.getByRole("region", { name: "Light のDevice情報" })).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("region", { name: "Light のDevice情報" })).not.toBeInTheDocument();
    expect(button).toHaveFocus();
    expect(button).toHaveAttribute("aria-expanded", "false");

    await user.keyboard("{Enter}");
    expect(screen.getByRole("region", { name: "Light のDevice情報" })).toBeInTheDocument();
    await user.keyboard(" ");
    expect(screen.queryByRole("region", { name: "Light のDevice情報" })).not.toBeInTheDocument();
  });

  it("provides a labelled, viewport-constrained panel aligned to the trigger edge", async () => {
    const user = userEvent.setup();
    render(<InfoPopover subject="Command Power" title="Power のCommand情報"><p>Format: code</p></InfoPopover>);

    const button = screen.getByRole("button", { name: "Command Powerの情報" });
    vi.spyOn(button, "getBoundingClientRect").mockReturnValue({
      x: 990, y: 20, top: 20, right: 1014, bottom: 50, left: 990, width: 24, height: 30,
      toJSON: () => ({}),
    } as DOMRect);
    await user.click(button);
    const panel = screen.getByRole("region", { name: "Power のCommand情報" });
    vi.spyOn(panel, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, top: 0, right: 352, bottom: 120, left: 0, width: 352, height: 120,
      toJSON: () => ({}),
    } as DOMRect);
    window.dispatchEvent(new Event("resize"));
    expect(button).toHaveAttribute("aria-controls", panel.id);
    expect(panel.style.left).toBe("660px");
    expect(panel.style.maxWidth).toBe("1000px");
    expect(panel).toHaveAccessibleName("Power のCommand情報");

    button.getBoundingClientRect = () => ({
      x: 8, y: 20, top: 20, right: 32, bottom: 50, left: 8, width: 24, height: 30,
      toJSON: () => ({}),
    } as DOMRect);
    window.dispatchEvent(new Event("resize"));
    expect(panel.style.left).toBe("12px");
  });

  it("places an overflowing panel above its trigger or scrolls it below without covering the trigger", async () => {
    const user = userEvent.setup();
    render(<InfoPopover subject="Command Power" title="Power のCommand情報"><p>Format: code</p></InfoPopover>);
    const button = screen.getByRole("button", { name: "Command Powerの情報" });
    vi.spyOn(button, "getBoundingClientRect").mockReturnValue({
      top: 480, bottom: 524, right: 200,
    } as DOMRect);
    await user.click(button);
    const panel = screen.getByRole("region", { name: "Power のCommand情報" });
    vi.spyOn(panel, "getBoundingClientRect").mockReturnValue({ width: 300, height: 300 } as DOMRect);
    window.dispatchEvent(new Event("resize"));
    expect(panel.style.top).toBe("178px");
    expect(panel.style.maxHeight).toBe("466px");

    vi.stubGlobal("innerHeight", 400);
    vi.spyOn(button, "getBoundingClientRect").mockReturnValue({
      top: 150, bottom: 194, right: 200,
    } as DOMRect);
    window.dispatchEvent(new Event("resize"));
    expect(panel.style.top).toBe("196px");
    expect(panel.style.maxHeight).toBe("192px");
    vi.unstubAllGlobals();
  });
});
