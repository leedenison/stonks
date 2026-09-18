import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import { StatementSummarySchema } from "@/gen/statement/v1/statement_pb";
import { StateChip } from "./state-chip";

function summary(state: RunState, rejected = 0, startedAt?: Date) {
  return create(StatementSummarySchema, {
    run: create(RunSchema, {
      state,
      startedAt: startedAt && timestampFromDate(startedAt),
    }),
    rejected,
  });
}

describe("StateChip", () => {
  afterEach(() => vi.useRealTimers());

  it("marks each outcome", () => {
    const cases: [RunState, number, string, string][] = [
      [RunState.PENDING, 0, "pending", "Pending"],
      [RunState.COMPLETED, 0, "completed", "Completed"],
      [RunState.COMPLETED, 2, "rejections", "Rejections"],
      [RunState.FAILED, 0, "failed", "Failed"],
      [RunState.INTERRUPTED, 0, "failed", "Failed"],
    ];
    for (const [state, rejected, outcome, label] of cases) {
      const { unmount } = render(
        <StateChip summary={summary(state, rejected)} />,
      );
      const chip = screen.getByTestId("state-chip");
      expect(chip.getAttribute("data-state")).toBe(outcome);
      expect(chip.textContent).toBe(label);
      unmount();
    }
  });

  it("shows the time since a running run started, ticking", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-18T10:01:05Z"));
    render(
      <StateChip
        summary={summary(RunState.RUNNING, 0, new Date("2026-09-18T10:00:00Z"))}
      />,
    );
    const chip = screen.getByTestId("state-chip");
    expect(chip.getAttribute("data-state")).toBe("running");
    expect(chip.textContent).toBe("Running1m 05s");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(chip.textContent).toBe("Running1m 06s");
  });
});
