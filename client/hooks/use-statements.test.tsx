import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
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
import { authWrapper } from "@/lib/test-utils";
import { useRun } from "./use-run";
import { useStatement } from "./use-statement";
import { useStatements } from "./use-statements";

const live = create(GetSessionResponseSchema, {
  user: create(UserSchema, {
    id: "u1",
    email: "a@example.com",
    role: Role.USER,
  }),
  session: create(SessionSchema, {
    expiresAt: timestampFromDate(new Date("2026-09-16T00:00:00Z")),
  }),
});

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

function transportWith(
  listStatements: () => ReturnType<
    typeof create<typeof ListStatementsResponseSchema>
  >,
  getRun = runAfter(0),
) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: () => live });
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
    let reads = 0;
    const listStatements = vi.fn(() =>
      create(ListStatementsResponseSchema, {
        statements: [
          summary("r1", ++reads > 2 ? RunState.COMPLETED : RunState.PENDING),
          summary("r0", RunState.COMPLETED),
        ],
      }),
    );
    const { result } = renderHook(() => useStatements(10), {
      wrapper: authWrapper(transportWith(listStatements)),
    });
    await waitFor(() =>
      expect(result.current.data?.statements[0].run?.state).toBe(
        RunState.COMPLETED,
      ),
    );
    const settled = listStatements.mock.calls.length;
    expect(settled).toBeGreaterThanOrEqual(3);
    await new Promise((r) => setTimeout(r, 60));
    expect(listStatements).toHaveBeenCalledTimes(settled);
  });
});

describe("useRun", () => {
  it("polls until the run is terminal", async () => {
    const getRun = runAfter(2);
    const { result } = renderHook(() => useRun("r1", 10), {
      wrapper: authWrapper(
        transportWith(() => create(ListStatementsResponseSchema, {}), getRun),
      ),
    });
    await waitFor(() =>
      expect(result.current.data?.run?.state).toBe(RunState.COMPLETED),
    );
    const settled = getRun.mock.calls.length;
    expect(settled).toBe(3);
    await new Promise((r) => setTimeout(r, 60));
    expect(getRun).toHaveBeenCalledTimes(settled);
  });
});

describe("useStatement", () => {
  it("reads the statement and its items", async () => {
    const { result } = renderHook(() => useStatement("r7"), {
      wrapper: authWrapper(
        transportWith(() => create(ListStatementsResponseSchema, {})),
      ),
    });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    expect(result.current.data?.statement?.run?.id).toBe("r7");
    expect(result.current.data?.items[0].reason).toBe("no key");
  });
});
