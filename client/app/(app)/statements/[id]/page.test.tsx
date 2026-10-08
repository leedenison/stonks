import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import {
  GetStatementResponseSchema,
  RowSchema,
  StatementItemSchema,
  StatementService,
  StatementSummarySchema,
} from "@/gen/statement/v1/statement_pb";
import {
  AssetClass,
  Broker,
  IdentifierSchema,
  IdentifierType,
  ResolutionItemSchema,
  ResolutionOutcome,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { pollInterval } from "@/lib/run";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import StatementPage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "r1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

const live = liveSession();

afterEach(() => vi.useRealTimers());

// advance moves the faked clock by ms and lets the query's answer reach the
// page. The faked clock runs a zero-delay timer one millisecond late when
// a tick schedules it, so the answer needs a few one-millisecond rounds.
async function advance(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
  for (let i = 0; i < 3; i++) {
    await act(() => vi.advanceTimersByTimeAsync(1));
  }
}

// keys returns a statement's three keys: cash matched, one key nothing
// recognised and one whose datasource failed. While the run is live, two of
// them are listed with no outcome yet.
function keys(resolved: boolean) {
  const gbp = create(StatedKeySchema, {
    assetClass: AssetClass.CASH,
    currency: "GBP",
  });
  const isin = create(StatedKeySchema, {
    identifiers: [
      create(IdentifierSchema, {
        type: IdentifierType.ISIN,
        value: "US0378331005",
      }),
      create(IdentifierSchema, {
        type: IdentifierType.BROKER_DESCRIPTION,
        domain: "ibkr",
        value: "APPLE INC",
      }),
    ],
  });
  const bare = create(StatedKeySchema, {
    identifiers: [
      create(IdentifierSchema, {
        type: IdentifierType.BROKER_DESCRIPTION,
        domain: "fidelity_uk",
        value: "Baillie Gifford",
      }),
    ],
  });
  if (!resolved) {
    return [
      create(ResolutionItemSchema, { statedKey: gbp, statedKeyId: "k-gbp" }),
      create(ResolutionItemSchema, { statedKey: isin, statedKeyId: "k-isin" }),
    ];
  }
  return [
    create(ResolutionItemSchema, {
      statedKey: gbp,
      statedKeyId: "k-gbp",
      outcome: ResolutionOutcome.MATCHED,
    }),
    create(ResolutionItemSchema, {
      statedKey: isin,
      statedKeyId: "k-isin",
      outcome: ResolutionOutcome.UNAVAILABLE,
      reasons: ["openfigi: failed: 429"],
    }),
    create(ResolutionItemSchema, {
      statedKey: bare,
      statedKeyId: "k-bare",
      outcome: ResolutionOutcome.UNRECOGNISED,
      reasons: ["openfigi: 0 candidates", "massive: 1 candidate"],
    }),
  ];
}

function response(
  state: RunState,
  rejected: number,
  error?: string,
  withKeys = false,
) {
  const gbp = create(StatedKeySchema, {
    assetClass: AssetClass.CASH,
    currency: "GBP",
  });
  return create(GetStatementResponseSchema, {
    keys: withKeys ? keys(state === RunState.COMPLETED) : [],
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

function serving(
  getStatement: ServiceImpl<typeof StatementService>["getStatement"],
) {
  return transportWith(live, ({ service }) => {
    service(StatementService, { getStatement });
  });
}

describe("StatementPage", () => {
  it("shows the summary and the rejections grouped by reason", async () => {
    const getStatement = vi.fn(() => response(RunState.COMPLETED, 2));
    renderWithAuth(<StatementPage />, serving(getStatement));
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
    vi.useFakeTimers();
    let reads = 0;
    const getStatement = vi.fn(() =>
      response(++reads > 2 ? RunState.COMPLETED : RunState.RUNNING, 0),
    );
    renderWithAuth(<StatementPage />, serving(getStatement));
    await advance(0);
    expect(getStatement).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("status").textContent).toContain("being ingested");
    await advance(pollInterval * 2);
    expect(getStatement).toHaveBeenCalledTimes(3);
    const done = screen.getByRole("status");
    expect(done.textContent).toBe("All rows are valid.");
    expect(done.getAttribute("data-tone")).toBe("positive");
    await advance(pollInterval * 3);
    expect(getStatement).toHaveBeenCalledTimes(3);
  });

  it("lists each key with what its latest resolution made of it", async () => {
    renderWithAuth(
      <StatementPage />,
      serving(() => response(RunState.COMPLETED, 0, undefined, true)),
    );
    await waitFor(() =>
      expect(screen.getByTestId("statement-keys")).toBeTruthy(),
    );
    const state = (id: string) =>
      screen
        .getByTestId(`key-row-${id}`)
        .querySelector("[data-testid=resolution-chip]")
        ?.getAttribute("data-state");
    expect(state("k-gbp")).toBe("matched");
    expect(state("k-isin")).toBe("unavailable");
    expect(state("k-bare")).toBe("unrecognised");
    const isin = screen.getByTestId("key-row-k-isin");
    expect(isin.textContent).toContain("US0378331005");
    expect(isin.textContent).toContain("APPLE INC");
    expect(isin.textContent).toContain("openfigi: failed: 429");
    const parts = [
      ...screen.getByTestId("key-row-k-bare").querySelectorAll("li"),
    ].map((li) => li.textContent);
    expect(parts).toEqual(["openfigi: 0 candidates", "massive: 1 candidate"]);
  });

  it("shows a key as resolving while the run is live", async () => {
    renderWithAuth(
      <StatementPage />,
      serving(() => response(RunState.RUNNING, 0, undefined, true)),
    );
    await waitFor(() =>
      expect(screen.getByTestId("statement-keys")).toBeTruthy(),
    );
    const chips = screen.getAllByTestId("resolution-chip");
    expect(chips.map((c) => c.getAttribute("data-state"))).toEqual([
      "resolving",
      "resolving",
    ]);
  });

  it("shows a failed run's error", async () => {
    renderWithAuth(
      <StatementPage />,
      serving(() => response(RunState.FAILED, 0, "database gone")),
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
      serving(() => {
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
      serving(() => {
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
