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
  GetStmtResponseSchema,
  ListStmtsResponseSchema,
  StmtItemSchema,
  StmtService,
  StmtSummarySchema,
} from "@/gen/stmt/v1/stmt_pb";
import { pollInterval } from "@/lib/run";
import { authWrapper, liveSession, transportWith } from "@/lib/test-utils";
import { useRun } from "./use-run";
import { useStmt } from "./use-stmt";
import { useStmts } from "./use-stmts";

const live = liveSession();

afterEach(() => vi.useRealTimers());

// advance moves the faked clock by ms and lets the query's answer reach the
// hook. The faked clock runs a zero-delay timer one millisecond late when
// a tick schedules it, so the answer needs a few one-millisecond rounds.
async function advance(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
  for (let i = 0; i < 3; i++) {
    await act(() => vi.advanceTimersByTimeAsync(1));
  }
}

function summary(id: string, state: RunState, live = false) {
  return create(StmtSummarySchema, {
    run: create(RunSchema, { id, state }),
    rows: 3,
    live,
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
  listStmts: () => ReturnType<typeof create<typeof ListStmtsResponseSchema>>,
  getRun = runAfter(0),
) {
  return transportWith(live, ({ service }) => {
    service(StmtService, {
      listStmts,
      getStmt: (req) =>
        create(GetStmtResponseSchema, {
          stmt: summary(req.runId, RunState.COMPLETED),
          items: [create(StmtItemSchema, { ordinal: 2, reason: "no key" })],
        }),
    });
    service(RunService, { getRun });
  });
}

describe("useStmts", () => {
  it("lists the statements and stops polling once none is live", async () => {
    vi.useFakeTimers();
    let reads = 0;
    // r1's run completes on the second read and r1 settles on the third.
    const listStmts = vi.fn(() => {
      reads++;
      return create(ListStmtsResponseSchema, {
        stmts: [
          summary(
            "r1",
            reads > 1 ? RunState.COMPLETED : RunState.PENDING,
            reads < 3,
          ),
          summary("r0", RunState.COMPLETED),
        ],
      });
    });
    const { result } = renderHook(() => useStmts(), {
      wrapper: authWrapper(serving(listStmts)),
    });
    await advance(0);
    expect(listStmts).toHaveBeenCalledTimes(1);
    expect(result.current.data?.stmts[0].run?.state).toBe(RunState.PENDING);
    await advance(pollInterval);
    expect(listStmts).toHaveBeenCalledTimes(2);
    expect(result.current.data?.stmts[0].run?.state).toBe(RunState.COMPLETED);
    await advance(pollInterval);
    expect(listStmts).toHaveBeenCalledTimes(3);
    expect(result.current.data?.stmts[0].live).toBe(false);
    await advance(pollInterval * 3);
    expect(listStmts).toHaveBeenCalledTimes(3);
  });
});

describe("useRun", () => {
  it("polls until the run is terminal", async () => {
    vi.useFakeTimers();
    const getRun = runAfter(2);
    const { result } = renderHook(() => useRun("r1"), {
      wrapper: authWrapper(
        serving(() => create(ListStmtsResponseSchema, {}), getRun),
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

describe("useStmt", () => {
  it("polls while the statement is live and stops once it is not", async () => {
    vi.useFakeTimers();
    let reads = 0;
    const getStmt = vi.fn(() =>
      create(GetStmtResponseSchema, {
        stmt: summary("r7", RunState.COMPLETED, ++reads < 3),
      }),
    );
    const transport = transportWith(live, ({ service }) => {
      service(StmtService, { getStmt });
    });
    const { result } = renderHook(() => useStmt("r7"), {
      wrapper: authWrapper(transport),
    });
    await advance(0);
    expect(getStmt).toHaveBeenCalledTimes(1);
    expect(result.current.data?.stmt?.live).toBe(true);
    await advance(pollInterval * 2);
    expect(getStmt).toHaveBeenCalledTimes(3);
    expect(result.current.data?.stmt?.live).toBe(false);
    await advance(pollInterval * 3);
    expect(getStmt).toHaveBeenCalledTimes(3);
  });

  it("reads the statement and its items", async () => {
    const { result } = renderHook(() => useStmt("r7"), {
      wrapper: authWrapper(serving(() => create(ListStmtsResponseSchema, {}))),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.stmt?.run?.id).toBe("r7");
    expect(result.current.data?.items[0].reason).toBe("no key");
  });
});
