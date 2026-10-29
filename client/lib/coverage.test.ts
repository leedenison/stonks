import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  IntervalSchema,
  TickerSchema,
  TickerValidity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { AssertionItemSchema } from "@/gen/admin/run/v1/run_pb";
import { bounds, cutDays, dayNumber, intervalAt } from "./coverage";

describe("coverage", () => {
  it("counts days and finds the interval holding a day", () => {
    expect(dayNumber("1970-01-02")).toBe(1);
    expect(dayNumber("2026-03-10") - dayNumber("2026-03-01")).toBe(9);
    const intervals = [
      create(IntervalSchema, {
        period: { before: "2026-03-10" },
        instrId: "i1",
        validity: TickerValidity.PROVISIONAL,
      }),
      create(IntervalSchema, {
        period: { from: "2026-03-10", before: "2026-03-21" },
        instrId: "i1",
        validity: TickerValidity.CONFIRMED,
      }),
    ];
    expect(intervalAt(intervals, "2026-03-01")?.validity).toBe(
      TickerValidity.PROVISIONAL,
    );
    expect(intervalAt(intervals, "2026-03-10")?.validity).toBe(
      TickerValidity.CONFIRMED,
    );
    expect(intervalAt(intervals, "2026-03-20")?.validity).toBe(
      TickerValidity.CONFIRMED,
    );
    expect(intervalAt(intervals, "2026-03-21")).toBeUndefined();
  });

  it("bounds the timeline by every dated fact, leaving open bounds out", () => {
    expect(bounds(create(TickerSchema, {}))).toBeUndefined();
    const t = create(TickerSchema, {
      txnDays: ["2026-03-05"],
      intervals: [
        create(IntervalSchema, {
          period: { from: "2026-03-10" },
          instrId: "i1",
        }),
      ],
      gaps: [{ period: { from: "2026-03-05", before: "2026-03-31" } }],
    });
    expect(bounds(t)).toEqual({ from: "2026-03-05", before: "2026-03-31" });
    expect(bounds(create(TickerSchema, { txnDays: ["2026-03-05"] }))).toEqual({
      from: "2026-03-05",
      before: "2026-03-06",
    });
  });

  it("cuts at each bound that is an edge, and bounds spans the periods", () => {
    const period = { period: { from: "2026-03-05", before: "2026-03-10" } };
    expect(cutDays(create(AssertionItemSchema, period))).toEqual([]);
    expect(
      cutDays(create(AssertionItemSchema, { ...period, edgeFrom: true })),
    ).toEqual(["2026-03-05"]);
    expect(
      cutDays(
        create(AssertionItemSchema, {
          ...period,
          edgeFrom: true,
          edgeBefore: true,
        }),
      ),
    ).toEqual(["2026-03-05", "2026-03-10"]);
    const t = create(TickerSchema, {
      txnDays: ["2026-03-01"],
      assertions: [create(AssertionItemSchema, period)],
    });
    expect(bounds(t)).toEqual({ from: "2026-03-01", before: "2026-03-10" });
  });
});
