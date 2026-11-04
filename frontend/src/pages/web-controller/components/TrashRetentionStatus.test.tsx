import { render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it } from "vitest";

import { TrashRetentionStatus } from "./TrashRetentionStatus";

function timestamp(offsetMs: number): string {
  return new Date(Date.now() + offsetMs).toISOString();
}

function renderRetention(overrides: Partial<ComponentProps<typeof TrashRetentionStatus>> = {}) {
  return render(
    <TrashRetentionStatus
      deletedAt={timestamp(-86_400_000)}
      eligibleAfter={timestamp(86_400_000)}
      lastCheckedAt={timestamp(-60_000)}
      restoreAvailable
      {...overrides}
    />,
  );
}

describe("TrashRetentionStatus", () => {
  it("treats unset or invalid retention timestamps as unknown", () => {
    const view = renderRetention({ deletedAt: null, eligibleAfter: null });
    expect(screen.getByRole("alert")).toHaveTextContent("保持期限が未設定または日時形式不正");
    expect(screen.getAllByText("未設定または日時形式不正").length).toBeGreaterThanOrEqual(2);

    view.rerender(
      <TrashRetentionStatus
        deletedAt={timestamp(-86_400_000)}
        eligibleAfter="not-a-date"
        lastCheckedAt={null}
        restoreAvailable={false}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("保持期限が未設定または日時形式不正");
    expect(screen.queryByText(/保持期限経過/)).not.toBeInTheDocument();
  });

  it("shows a holding state, UTC timestamps, recorded restore eligibility, and CLI-only final-delete guidance", () => {
    renderRetention();

    expect(screen.getByRole("status")).toHaveTextContent("保持中（参考表示）");
    expect(screen.getByText(/Backend最終clock確認/)).toBeInTheDocument();
    expect(screen.getByText(/可能（restore dry-runで再確認）/)).toBeInTheDocument();
    expect(screen.getByText(/最終削除はBackend host上で運用者がManual CLI/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /最終削除|final-delete/ })).not.toBeInTheDocument();
  });

  it("shows expiration only as advisory and includes restore-unavailable status", () => {
    renderRetention({ eligibleAfter: timestamp(-120_000), restoreAvailable: false });

    expect(screen.getByRole("status")).toHaveTextContent("保持期限経過（参考表示）");
    expect(screen.getByText(/不可（現在のentry記録）/)).toBeInTheDocument();
    expect(screen.getByText(/期限経過は削除の承認ではありません/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /最終削除|final-delete/ })).not.toBeInTheDocument();
  });

  it("withholds the phase judgement and warns when the browser clock predates Backend's last check", () => {
    renderRetention({ lastCheckedAt: timestamp(60_000) });

    expect(screen.getByRole("alert")).toHaveTextContent("clock warning");
    expect(screen.getByRole("alert")).toHaveTextContent("Backend最終確認時刻より前");
    expect(screen.queryByText(/保持中（参考表示）|保持期限経過（参考表示）/)).not.toBeInTheDocument();
  });

  it("warns when the Backend clock checkpoint itself is malformed", () => {
    renderRetention({ lastCheckedAt: "invalid-checkpoint" });

    expect(screen.getByRole("alert")).toHaveTextContent("clock warning");
    expect(screen.getByText("日時形式不正")).toBeInTheDocument();
  });
});
