import { type Run, RunState } from "@/gen/run/v1/run_pb";
import type { StatementSummary } from "@/gen/statement/v1/statement_pb";

// What a run's chip shows. A completed run with rejected rows is its own
// outcome, since the user has something to look at. Interrupted reads as
// failed: the work stopped short and uploading again is the recovery either
// way.
export type Outcome =
  "pending" | "running" | "completed" | "rejections" | "failed";

export function isTerminal(state: RunState | undefined): boolean {
  return (
    state === RunState.COMPLETED ||
    state === RunState.FAILED ||
    state === RunState.INTERRUPTED
  );
}

export function outcome(s: StatementSummary): Outcome {
  return runOutcome(s.run, s.rejected);
}

// runOutcome is the outcome of a run whose rejected items are counted
// elsewhere, or not at all.
export function runOutcome(run: Run | undefined, rejected = 0): Outcome {
  switch (run?.state) {
    case RunState.RUNNING:
      return "running";
    case RunState.COMPLETED:
      return rejected > 0 ? "rejections" : "completed";
    case RunState.FAILED:
    case RunState.INTERRUPTED:
      return "failed";
    default:
      return "pending";
  }
}

// How often a live run is read, in milliseconds. A statement takes seconds
// at most, so nothing finer is worth the requests.
export const pollInterval = 2000;

export function anyLive(states: (RunState | undefined)[]): boolean {
  return states.some((s) => !isTerminal(s));
}
