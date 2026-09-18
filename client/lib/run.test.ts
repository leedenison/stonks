import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import { StatementSummarySchema } from "@/gen/statement/v1/statement_pb";
import { anyLive, isTerminal, outcome } from "./run";

function summary(state: RunState, rejected = 0) {
  return create(StatementSummarySchema, {
    run: create(RunSchema, { state }),
    rejected,
  });
}

describe("run", () => {
  it("knows the terminal states", () => {
    expect(isTerminal(RunState.PENDING)).toBe(false);
    expect(isTerminal(RunState.RUNNING)).toBe(false);
    expect(isTerminal(RunState.COMPLETED)).toBe(true);
    expect(isTerminal(RunState.FAILED)).toBe(true);
    expect(isTerminal(RunState.INTERRUPTED)).toBe(true);
    expect(isTerminal(undefined)).toBe(false);
  });

  it("folds state and rejections into an outcome", () => {
    expect(outcome(summary(RunState.PENDING))).toBe("pending");
    expect(outcome(summary(RunState.RUNNING))).toBe("running");
    expect(outcome(summary(RunState.COMPLETED))).toBe("completed");
    expect(outcome(summary(RunState.COMPLETED, 3))).toBe("rejections");
    expect(outcome(summary(RunState.FAILED))).toBe("failed");
    expect(outcome(summary(RunState.INTERRUPTED))).toBe("failed");
    expect(outcome(create(StatementSummarySchema, {}))).toBe("pending");
  });

  it("is live while any run is not terminal", () => {
    expect(anyLive([])).toBe(false);
    expect(anyLive([RunState.COMPLETED, RunState.FAILED])).toBe(false);
    expect(anyLive([RunState.COMPLETED, RunState.PENDING])).toBe(true);
  });
});
