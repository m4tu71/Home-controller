import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { fetchDataHealth } from "../api/controller";
import type { IRCommand } from "../api/controller";
import { IRCommandCard } from "../pages/web-controller/components/IRCommandCard";

const command: IRCommand = {
  id: "power",
  name: "Power",
  format: "raw",
  count: 2,
  carrier_khz: 38,
  timings_us: [100, 200],
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Frontend test runner smoke checks", () => {
  it("renders an IR Command operation button in the DOM", () => {
    render(
      <IRCommandCard command={command} isSelected={false} carrierKHz={38} disabled={false} onSelectCommand={vi.fn()} />,
    );

    expect(screen.getByRole("button", { name: /Power \(power\).*raw.*送信可能/ })).toBeInTheDocument();
    expect(screen.getByText("power · raw")).toBeInTheDocument();
  });

  it("handles a user selecting an IR Command", async () => {
    const onSelectCommand = vi.fn();
    const user = userEvent.setup();
    render(
      <IRCommandCard
        command={command}
        isSelected={false}
        carrierKHz={38}
        disabled={false}
        onSelectCommand={onSelectCommand}
      />,
    );

    await user.click(screen.getByRole("button", { name: /Power \(power\)/, pressed: false }));

    expect(onSelectCommand).toHaveBeenCalledWith(command);
  });

  it("surfaces an API failure from the controller client", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "maintenance unavailable" }), {
          status: 503,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(fetchDataHealth()).rejects.toThrow("maintenance unavailable");
  });
});
