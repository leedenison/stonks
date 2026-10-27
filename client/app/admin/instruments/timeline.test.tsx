import { create } from "@bufbuild/protobuf";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  IdentifierEventAction,
  TickerSchema,
  TickerValidity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { AssertionItemSchema } from "@/gen/admin/run/v1/run_pb";
import { Timeline, ticks } from "./timeline";

const ticker = { market: "US", value: "ACME" };

// A ticker with every kind of fact, so each lane has a mark. One datasource
// covers the whole market.
const acme = create(TickerSchema, {
  ticker,
  transactionDays: ["2024-02-01", "2025-06-01"],
  coverage: [
    {
      datasource: "alpha",
      market: "US",
      value: "ACME",
      coveredFrom: "2024-06-01",
      coveredBefore: "2026-03-01",
    },
    {
      datasource: "beta",
      market: "US",
      coveredFrom: "2025-01-01",
      coveredBefore: "2026-03-01",
    },
  ],
  assertions: [
    create(AssertionItemSchema, {
      id: "a1",
      ticker,
      assertedOn: "2024-06-01",
      instrumentId: "i1",
      source: { case: "datasource", value: "alpha" },
    }),
    create(AssertionItemSchema, {
      id: "a2",
      ticker,
      assertedOn: "2026-03-01",
      source: { case: "attestedBy", value: "admin@example.com" },
    }),
  ],
  events: [
    {
      effectiveOn: "2024-06-01",
      action: IdentifierEventAction.RENAMED,
      value: "OLD",
      toValue: "ACME",
    },
  ],
  intervals: [
    {
      validFrom: "2024-06-01",
      validBefore: "2026-03-01",
      instrumentId: "i1",
      validity: TickerValidity.CONFIRMED,
    },
  ],
  gaps: [
    {
      unconfirmedFrom: "2024-02-01",
      unconfirmedBefore: "2024-06-01",
      keys: 1,
      transactions: 1,
    },
  ],
});

describe("Timeline", () => {
  it("draws one lane per kind of fact and one per datasource", () => {
    render(<Timeline ticker={acme} />);
    const svg = screen.getByTestId("coverage-timeline");
    const lane = (key: string) => svg.querySelector(`[data-lane="${key}"]`);
    expect(lane("transactions")?.querySelectorAll("rect")).toHaveLength(2);
    expect(lane("validity")?.querySelectorAll("rect")).toHaveLength(1);
    expect(
      lane("validity")?.querySelector("rect")?.getAttribute("data-validity"),
    ).toBe("confirmed");
    expect(lane("gaps")?.querySelectorAll("rect")).toHaveLength(1);
    const markers = lane("assertions")?.querySelectorAll("circle") ?? [];
    expect(markers).toHaveLength(2);
    expect(markers[0].getAttribute("data-attested")).toBeNull();
    expect(markers[1].getAttribute("data-attested")).toBe("true");
    expect(markers[1].getAttribute("data-negative")).toBe("true");
    expect(lane("events")?.querySelectorAll("polygon")).toHaveLength(1);
    expect(lane("coverage-alpha")?.querySelectorAll("rect")).toHaveLength(1);
    expect(
      lane("coverage-beta")?.querySelector("rect")?.getAttribute("data-market"),
    ).toBe("true");
    // The range spans more than two years, so the axis is labelled by year.
    expect(lane("axis")?.textContent).toBe("20252026");
  });

  it("says when there is nothing to draw", () => {
    render(<Timeline ticker={create(TickerSchema, { ticker })} />);
    expect(screen.getByTestId("coverage-timeline").textContent).toBe(
      "Nothing to show.",
    );
  });

  it("ticks by month under two years and by year beyond", () => {
    expect(ticks("2025-11-15", "2026-02-10").map((t) => t.label)).toEqual([
      "2025-12",
      "2026-01",
      "2026-02",
    ]);
    expect(ticks("2023-06-01", "2026-03-01").map((t) => t.label)).toEqual([
      "2024",
      "2025",
      "2026",
    ]);
  });
});
