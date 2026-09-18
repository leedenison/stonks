import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { createRouterTransport } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  AuthService,
  GetSessionResponseSchema,
  Role,
  SessionSchema,
  UserSchema,
} from "@/gen/auth/v1/auth_pb";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import {
  GetStatementResponseSchema,
  ListStatementsResponseSchema,
  StatementItemSchema,
  StatementService,
  StatementSummarySchema,
} from "@/gen/statement/v1/statement_pb";
import { Broker } from "@/gen/type/v1/type_pb";
import { useActivity } from "@/contexts/activity-context";
import { renderWithAuth } from "@/lib/test-utils";
import { ActivitySheet } from "./activity-sheet";

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

function summary(
  id: string,
  state: RunState,
  rejected: number,
  finishedAt?: Date,
) {
  return create(StatementSummarySchema, {
    run: create(RunSchema, {
      id,
      state,
      createdAt: timestampFromDate(new Date("2026-09-18T10:00:00Z")),
      finishedAt: finishedAt && timestampFromDate(finishedAt),
    }),
    broker: Broker.IBKR,
    orderFrom: "2024-01-01",
    orderBefore: "2024-04-01",
    rows: 20,
    rejected,
  });
}

function transportWith(list: ReturnType<typeof summary>[]) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: () => live });
    service(StatementService, {
      listStatements: () =>
        create(ListStatementsResponseSchema, { statements: list }),
      getStatement: (req) =>
        create(GetStatementResponseSchema, {
          statement: list.find((s) => s.run?.id === req.runId),
          items: [
            create(StatementItemSchema, {
              ordinal: 16,
              reason: "order date outside the claimed period",
            }),
            create(StatementItemSchema, { ordinal: 19, reason: "no currency" }),
          ],
        }),
    });
  });
}

// Controls drives the sheet and shows the badge, as the top bar would.
function Controls() {
  const { open, close, badge } = useActivity();
  return (
    <>
      <button type="button" onClick={open}>
        open
      </button>
      <button type="button" onClick={close}>
        close
      </button>
      <p data-testid="badge">{badge}</p>
    </>
  );
}

describe("ActivitySheet", () => {
  beforeEach(() => localStorage.clear());

  it("lists the runs and opens one to its rejections", async () => {
    const list = [
      summary("r2", RunState.PENDING, 0),
      summary("r1", RunState.COMPLETED, 2, new Date("2026-09-18T10:00:05Z")),
    ];
    renderWithAuth(
      <>
        <Controls />
        <ActivitySheet />
      </>,
      transportWith(list),
    );
    fireEvent.click(screen.getByText("open"));
    await waitFor(() =>
      expect(screen.getByTestId("activity-item-r1")).toBeTruthy(),
    );
    const items = screen.getAllByTestId(/^activity-item-/);
    expect(items.map((i) => i.getAttribute("data-testid"))).toEqual([
      "activity-item-r2",
      "activity-item-r1",
    ]);
    expect(
      screen
        .getByTestId("activity-item-r1")
        .querySelector('[data-testid="state-chip"]')
        ?.getAttribute("data-state"),
    ).toBe("rejections");
    fireEvent.click(screen.getByTestId("activity-toggle-r1"));
    await waitFor(() =>
      expect(screen.getByTestId("rejection-group-0")).toBeTruthy(),
    );
    expect(screen.getAllByTestId(/^rejection-group-/).length).toBe(2);
    expect(screen.getByTestId("activity-open-r1").getAttribute("href")).toBe(
      "/statements/r1",
    );
    expect(screen.getByTestId("activity-all").getAttribute("href")).toBe(
      "/statements",
    );
  });

  it("badges runs finished since the sheet was last open, and never history", async () => {
    const list = [
      summary("r1", RunState.COMPLETED, 0, new Date("2026-09-18T10:00:05Z")),
    ];
    renderWithAuth(
      <>
        <Controls />
        <ActivitySheet />
      </>,
      transportWith(list),
    );
    fireEvent.click(screen.getByText("open"));
    await waitFor(() =>
      expect(screen.getByTestId("activity-item-r1")).toBeTruthy(),
    );
    expect(screen.getByTestId("badge").textContent).toBe("0");
    fireEvent.click(screen.getByText("close"));
    expect(screen.getByTestId("badge").textContent).toBe("0");
    expect(
      Number(localStorage.getItem("stonks.activity.seen.u1")),
    ).toBeGreaterThan(new Date("2026-09-18T10:00:05Z").getTime());
  });

  it("counts a run finished after the last opening", async () => {
    localStorage.setItem(
      "stonks.activity.seen.u1",
      String(new Date("2026-09-18T10:00:00Z").getTime()),
    );
    const list = [
      summary("r1", RunState.COMPLETED, 0, new Date("2026-09-18T10:00:05Z")),
      summary("r0", RunState.COMPLETED, 0, new Date("2026-09-18T09:00:00Z")),
    ];
    renderWithAuth(
      <>
        <Controls />
        <ActivitySheet />
      </>,
      transportWith(list),
    );
    await waitFor(() =>
      expect(screen.getByTestId("badge").textContent).toBe("1"),
    );
  });
});
