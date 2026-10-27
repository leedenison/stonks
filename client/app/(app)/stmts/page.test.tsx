import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RunSchema, RunState } from "@/gen/run/v1/run_pb";
import {
  ListStmtsResponseSchema,
  StmtService,
  StmtSummarySchema,
} from "@/gen/stmt/v1/stmt_pb";
import { Broker } from "@/gen/type/v1/type_pb";
import { UploadProvider } from "@/contexts/upload-context";
import { liveSession, renderWithAuth, transportWith } from "@/lib/test-utils";
import StmtsPage from "./page";

const page = (
  <UploadProvider>
    <StmtsPage />
  </UploadProvider>
);

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

const live = liveSession();

function serving(listStmts: ServiceImpl<typeof StmtService>["listStmts"]) {
  return transportWith(live, ({ service }) => {
    service(StmtService, { listStmts });
  });
}

const two = create(ListStmtsResponseSchema, {
  stmts: [
    create(StmtSummarySchema, {
      run: create(RunSchema, {
        id: "r2",
        state: RunState.PENDING,
        createdAt: timestampFromDate(new Date("2026-09-18T10:00:00Z")),
      }),
      broker: Broker.SCHWAB,
      orderFrom: "2024-01-01",
      orderBefore: "2025-01-01",
      rows: 40,
    }),
    create(StmtSummarySchema, {
      run: create(RunSchema, {
        id: "r1",
        state: RunState.COMPLETED,
        createdAt: timestampFromDate(new Date("2026-09-17T09:30:00Z")),
      }),
      broker: Broker.FIDELITY_UK,
      orderFrom: "2025-02-01",
      orderBefore: "2025-03-17",
      rows: 11,
      rejected: 3,
    }),
  ],
});

describe("StmtsPage", () => {
  it("shows skeleton rows while loading", () => {
    renderWithAuth(
      page,
      serving(() => new Promise(() => {})),
    );
    expect(screen.getByTestId("stmts-table")).toBeTruthy();
    expect(screen.getByTestId("skeleton-rows")).toBeTruthy();
  });

  it("shows the empty state without statements", async () => {
    renderWithAuth(
      page,
      serving(() => create(ListStmtsResponseSchema, {})),
    );
    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeTruthy());
    expect(screen.queryByTestId("stmts-table")).toBeNull();
  });

  it("lists the statements with their outcome and links each", async () => {
    renderWithAuth(
      page,
      serving(() => two),
    );
    await waitFor(() => expect(screen.getByTestId("stmt-row-r1")).toBeTruthy());
    const rows = screen.getAllByTestId(/^stmt-row-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "stmt-row-r2",
      "stmt-row-r1",
    ]);
    const r1 = screen.getByTestId("stmt-row-r1");
    expect(r1.textContent).toContain("2026-09-17 09:30 UTC");
    expect(r1.textContent).toContain("Fidelity UK");
    expect(r1.textContent).toContain("2025-02-01 to 2025-03-16");
    expect(r1.querySelector('[data-testid="stmt-rejected"]')?.textContent).toBe(
      "3",
    );
    expect(
      r1
        .querySelector('[data-testid="state-chip"]')
        ?.getAttribute("data-state"),
    ).toBe("rejections");
    expect(r1.querySelector("a")?.getAttribute("href")).toBe("/stmts/r1");
    expect(
      screen
        .getByTestId("stmt-row-r2")
        .querySelector('[data-testid="state-chip"]')
        ?.getAttribute("data-state"),
    ).toBe("pending");
  });

  it("offers a retry when the list fails", async () => {
    let calls = 0;
    renderWithAuth(
      page,
      serving(() => {
        if (++calls === 1) {
          throw new ConnectError("down", Code.Unavailable);
        }
        return two;
      }),
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.getByTestId("stmt-row-r1")).toBeTruthy());
  });
});
