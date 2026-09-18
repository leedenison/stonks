import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  Code,
  ConnectError,
  createRouterTransport,
  type ServiceImpl,
} from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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
  RowSchema,
  StatementItemSchema,
  StatementService,
  StatementSummarySchema,
} from "@/gen/statement/v1/statement_pb";
import { AssetClass, Broker, StatedKeySchema } from "@/gen/type/v1/type_pb";
import { renderWithAuth } from "@/lib/test-utils";
import StatementPage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "r1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

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

function response(state: RunState, rejected: number, error?: string) {
  const gbp = create(StatedKeySchema, {
    assetClass: AssetClass.CASH,
    currency: "GBP",
  });
  return create(GetStatementResponseSchema, {
    statement: create(StatementSummarySchema, {
      run: create(RunSchema, {
        id: "r1",
        state,
        error,
        createdAt: timestampFromDate(new Date("2026-09-17T09:30:00Z")),
      }),
      broker: Broker.FIDELITY_UK,
      orderFrom: "2025-02-01",
      orderBefore: "2025-03-17",
      rows: 4,
      rejected,
    }),
    items:
      rejected > 0
        ? [
            create(StatementItemSchema, {
              ordinal: 2,
              reason: "order date outside the claimed period",
              row: create(RowSchema, {
                key: gbp,
                orderDate: "2025-01-15",
                quantity: "-5.40",
              }),
            }),
            create(StatementItemSchema, {
              ordinal: 3,
              reason: "order date outside the claimed period",
              row: create(RowSchema, {
                key: gbp,
                orderDate: "2025-01-22",
                quantity: "100",
              }),
            }),
          ]
        : [],
  });
}

function transportWith(
  getStatement: ServiceImpl<typeof StatementService>["getStatement"],
) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getSession: () => live });
    service(StatementService, { getStatement });
  });
}

describe("StatementPage", () => {
  it("shows the summary and the rejections grouped by reason", async () => {
    const getStatement = vi.fn(() => response(RunState.COMPLETED, 2));
    renderWithAuth(<StatementPage />, transportWith(getStatement));
    await waitFor(() =>
      expect(screen.getByTestId("statement-summary")).toBeTruthy(),
    );
    expect(getStatement).toHaveBeenCalledWith(
      expect.objectContaining({ runId: "r1" }),
      expect.anything(),
    );
    expect(screen.getByTestId("page-title").textContent).toBe(
      "Fidelity UK upload @ 2026-09-17 09:30 UTC",
    );
    expect(screen.getByTestId("page-back")).toBeTruthy();
    const summary = screen.getByTestId("statement-summary");
    expect(summary.textContent).toContain("Fidelity UK");
    expect(summary.textContent).toContain("From2025-02-01");
    expect(summary.textContent).toContain("To2025-03-16");
    expect(summary.textContent).toContain("2026-09-17 09:30 UTC");
    expect(screen.getByTestId("statement-rejected").textContent).toBe("2");
    expect(screen.getByTestId("state-chip").getAttribute("data-state")).toBe(
      "rejections",
    );
    expect(screen.getAllByTestId(/^rejection-group-/).length).toBe(1);
    expect(screen.getByTestId("rejection-count-0").textContent).toBe("2");
    expect(screen.getByTestId("rejection-reason-0").textContent).toBe(
      "order date outside the claimed period",
    );
  });

  it("polls while the run is live and settles once it is terminal", async () => {
    let reads = 0;
    const getStatement = vi.fn(() =>
      response(++reads > 2 ? RunState.COMPLETED : RunState.RUNNING, 0),
    );
    renderWithAuth(<StatementPage />, transportWith(getStatement));
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain(
        "being ingested",
      ),
    );
    await waitFor(
      () =>
        expect(screen.getByRole("status").textContent).toContain("accepted"),
      { timeout: 8000 },
    );
    const settled = getStatement.mock.calls.length;
    await new Promise((r) => setTimeout(r, 100));
    expect(getStatement).toHaveBeenCalledTimes(settled);
  }, 10000);

  it("shows a failed run's error", async () => {
    renderWithAuth(
      <StatementPage />,
      transportWith(() => response(RunState.FAILED, 0, "database gone")),
    );
    await waitFor(() =>
      expect(screen.getByTestId("statement-summary").textContent).toContain(
        "database gone",
      ),
    );
    expect(screen.getByRole("alert").textContent).toContain("No rows");
  });

  it("says when there is no such statement", async () => {
    renderWithAuth(
      <StatementPage />,
      transportWith(() => {
        throw new ConnectError("no such statement", Code.NotFound);
      }),
    );
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain("no statement"),
    );
  });

  it("offers a retry on another failure", async () => {
    let calls = 0;
    renderWithAuth(
      <StatementPage />,
      transportWith(() => {
        if (++calls === 1) {
          throw new ConnectError("down", Code.Unavailable);
        }
        return response(RunState.COMPLETED, 0);
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(screen.getByTestId("statement-summary")).toBeTruthy(),
    );
  });
});
