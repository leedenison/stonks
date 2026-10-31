import { create } from "@bufbuild/protobuf";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  type ListRunsRequest,
  ListRunsResponseSchema,
  RunService,
} from "@/gen/admin/run/v1/run_pb";
import { Role } from "@/gen/auth/v1/auth_pb";
import { RunKind, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { pollInterval } from "@/lib/run";
import {
  authWrapper,
  liveSession,
  transportWith,
  userRun,
} from "@/lib/test-utils";
import { useLastSystemRun } from "./use-admin-runs";

afterEach(() => vi.useRealTimers());

// advance moves the faked clock by ms and lets the query's answer reach the
// hook.
async function advance(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
  for (let i = 0; i < 3; i++) {
    await act(() => vi.advanceTimersByTimeAsync(1));
  }
}

describe("useLastSystemRun", () => {
  it("reads the newest administrator run of the kind and polls until it ends", async () => {
    vi.useFakeTimers();
    const reqs: ListRunsRequest[] = [];
    const listRuns = vi.fn((req: ListRunsRequest) => {
      reqs.push(req);
      return create(ListRunsResponseSchema, {
        runs: [
          userRun({
            id: "x1",
            kind: RunKind.REASSERT_TICKERS,
            state: reqs.length > 1 ? RunState.COMPLETED : RunState.PENDING,
          }),
        ],
      });
    });
    const { result } = renderHook(
      () => useLastSystemRun(RunKind.REASSERT_TICKERS),
      {
        wrapper: authWrapper(
          transportWith(liveSession({ role: Role.ADMIN }), ({ service }) => {
            service(RunService, { listRuns });
          }),
        ),
      },
    );
    await advance(0);
    expect(listRuns).toHaveBeenCalledTimes(1);
    expect(reqs[0]).toMatchObject({
      kind: RunKind.REASSERT_TICKERS,
      trigger: RunTrigger.ADMINISTRATOR,
      pageSize: 1,
    });
    expect(result.current.data?.run?.state).toBe(RunState.PENDING);
    await advance(pollInterval);
    expect(listRuns).toHaveBeenCalledTimes(2);
    expect(result.current.data?.run?.state).toBe(RunState.COMPLETED);
    await advance(pollInterval * 3);
    expect(listRuns).toHaveBeenCalledTimes(2);
  });
});
