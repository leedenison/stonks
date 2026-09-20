import { create } from "@bufbuild/protobuf";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  GetRunResponseSchema,
  RunSchema,
  RunService,
  RunState,
} from "@/gen/run/v1/run_pb";
import {
  GetStatementResponseSchema,
  ListStatementsResponseSchema,
  StatementItemSchema,
  StatementService,
  StatementSummarySchema,
} from "@/gen/statement/v1/statement_pb";
import { pollInterval } from "@/lib/run";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useRun } from "./use-run";
import { useStatement } from "./use-statement";
import { useStatements } from "./use-statements";

const live = liveSession();

afterEach(() => vi.useRealTimers());

// advance moves the faked clock by ms, then lets what that starts settle.
// The query notifies its observers through zero-delay timers, which the
// faked clock lands one millisecond on when they are scheduled mid-tick, and
// each act boundary flushes the render a notification causes, so an answer
// takes a few one-millisecond rounds to reach the hook.
async function advance(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
  for (let i = 0; i < 3; i++) {
    await act(() => vi.advanceTimersByTimeAsync(1));
  }
}

function summary(id: string, state: RunState) {
  return create(StatementSummarySchema, {
    run: create(RunSchema, { id, state }),
    rows: 3,
  });
}

// A run that answers pending for the first n reads and completed after.
function runAfter(n: number) {
  let reads = 0;
  return vi.fn(() =>
    create(GetRunResponseSchema, {
      run: create(RunSchema, {
        id: "r1",
        state: ++reads > n ? RunState.COMPLETED : RunState.PENDING,
      }),
    }),
  );
}

function serving(
  listStatements: () => ReturnType<
    typeof create<typeof ListStatementsResponseSchema>
  >,
  getRun = runAfter(0),
) {
  return transportWith(live, ({ service }) => {
    service(StatementService, {
      listStatements,
      getStatement: (req) =>
        create(GetStatementResponseSchema, {
          statement: summary(req.runId, RunState.COMPLETED),
          items: [
            create(StatementItemSchema, { ordinal: 2, reason: "no key" }),
          ],
        }),
    });
    service(RunService, { getRun });
  });
}

describe("useStatements", () => {
  it("lists the statements and stops polling once every run is terminal", async () => {
    vi.useFakeTimers();
    let reads = 0;
    const listStatements = vi.fn(() =>
      create(ListStatementsResponseSchema, {
        statements: [
          summary("r1", ++reads > 2 ? RunState.COMPLETED : RunState.PENDING),
          summary("r0", RunState.COMPLETED),
        ],
      }),
    );
    const { result } = renderHook(() => useStatements(), {
      wrapper: authWrapper(serving(listStatements)),
    });
    await advance(0);
    expect(listStatements).toHaveBeenCalledTimes(1);
    expect(result.current.data?.statements[0].run?.state).toBe(
      RunState.PENDING,
    );
    await advance(pollInterval);
    expect(listStatements).toHaveBeenCalledTimes(2);
    await advance(pollInterval);
    expect(listStatements).toHaveBeenCalledTimes(3);
    expect(result.current.data?.statements[0].run?.state).toBe(
      RunState.COMPLETED,
    );
    await advance(pollInterval * 3);
    expect(listStatements).toHaveBeenCalledTimes(3);
  });
});

describe("useRun", () => {
  it("polls until the run is terminal", async () => {
    vi.useFakeTimers();
    const getRun = runAfter(2);
    const { result } = renderHook(() => useRun("r1"), {
      wrapper: authWrapper(
        serving(() => create(ListStatementsResponseSchema, {}), getRun),
      ),
    });
    await advance(0);
    expect(getRun).toHaveBeenCalledTimes(1);
    expect(result.current.data?.run?.state).toBe(RunState.PENDING);
    await advance(pollInterval * 2);
    expect(getRun).toHaveBeenCalledTimes(3);
    expect(result.current.data?.run?.state).toBe(RunState.COMPLETED);
    await advance(pollInterval * 3);
    expect(getRun).toHaveBeenCalledTimes(3);
  });
});

describe("useStatement", () => {
  it("reads the statement and its items", async () => {
    const { result } = renderHook(() => useStatement("r7"), {
      wrapper: authWrapper(
        serving(() => create(ListStatementsResponseSchema, {})),
      ),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.statement?.run?.id).toBe("r7");
    expect(result.current.data?.items[0].reason).toBe("no key");
  });
});
