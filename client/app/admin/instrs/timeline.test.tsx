import { create } from "@bufbuild/protobuf";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  TickerSchema,
  TickerValidity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { AssertionItemSchema } from "@/gen/admin/run/v1/run_pb";
import { Timeline, ticks } from "./timeline";

const ticker = { market: "US", value: "ACME" };

// A ticker with every kind of fact, so each lane has a mark.
const acme = create(TickerSchema, {
  ticker,
  txnDays: ["2024-02-01", "2025-06-01"],
  assertionCoverage: [
    {
      datasource: "alpha",
      period: { from: "2024-06-01", before: "2026-03-01" },
    },
    {
      datasource: "beta",
      period: { from: "2025-01-01", before: "2026-03-01" },
    },
  ],
  assertions: [
    create(AssertionItemSchema, {
      id: "a1",
      ticker,
      period: { from: "2024-06-01", before: "2025-06-01" },
      edgeFrom: true,
      edgeBefore: true,
      instrId: "i1",
      source: { case: "datasource", value: "alpha" },
    }),
    create(AssertionItemSchema, {
      id: "a3",
      ticker,
      period: { from: "2025-06-01", before: "2025-06-02" },
      instrId: "i1",
      source: { case: "datasource", value: "alpha" },
    }),
    create(AssertionItemSchema, {
      id: "a2",
      ticker,
      period: { from: "2026-03-01", before: "2026-03-02" },
      source: { case: "attestedBy", value: "admin@example.com" },
    }),
  ],
  intervals: [
    {
      period: { from: "2024-06-01", before: "2026-03-01" },
      instrId: "i1",
      validity: TickerValidity.CONFIRMED,
    },
  ],
  gaps: [
    {
      period: { from: "2024-02-01", before: "2024-06-01" },
      keys: 1,
      txns: 1,
    },
  ],
});

describe("Timeline", () => {
  it("draws one lane per kind of fact and one per datasource", () => {
    render(<Timeline ticker={acme} />);
    const svg = screen.getByTestId("coverage-timeline");
    const lane = (key: string) => svg.querySelector(`[data-lane="${key}"]`);
    expect(lane("txns")?.querySelectorAll("rect")).toHaveLength(2);
    expect(lane("validity")?.querySelectorAll("rect")).toHaveLength(1);
    expect(
      lane("validity")?.querySelector("rect")?.getAttribute("data-validity"),
    ).toBe("confirmed");
    expect(lane("gaps")?.querySelectorAll("rect")).toHaveLength(1);
    const markers = lane("assertions")?.querySelectorAll("rect") ?? [];
    expect(markers).toHaveLength(3);
    expect(markers[0].getAttribute("data-attested")).toBeNull();
    expect(markers[2].getAttribute("data-attested")).toBe("true");
    expect(markers[2].getAttribute("data-negative")).toBe("true");
    // An edge cuts at the start of the bound it marks.
    const cuts = [...(lane("edges")?.querySelectorAll("polygon") ?? [])].map(
      (p) => p.getAttribute("data-cut"),
    );
    expect(cuts).toEqual(["2024-06-01", "2025-06-01"]);
    expect(lane("coverage-alpha")?.querySelectorAll("rect")).toHaveLength(1);
    expect(lane("coverage-beta")?.querySelectorAll("rect")).toHaveLength(1);
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
