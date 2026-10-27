import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  IntervalSchema,
  TickerSchema,
  TickerValidity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { AssertionItemSchema, Edge } from "@/gen/admin/run/v1/run_pb";
import { bounds, cutDay, dayNumber, intervalAt } from "./coverage";

describe("coverage", () => {
  it("counts days and finds the interval holding a day", () => {
    expect(dayNumber("1970-01-02")).toBe(1);
    expect(dayNumber("2026-03-10") - dayNumber("2026-03-01")).toBe(9);
    const intervals = [
      create(IntervalSchema, {
        validBefore: "2026-03-10",
        instrId: "i1",
        validity: TickerValidity.PROVISIONAL,
      }),
      create(IntervalSchema, {
        validFrom: "2026-03-10",
        validBefore: "2026-03-21",
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
        create(IntervalSchema, { validFrom: "2026-03-10", instrId: "i1" }),
      ],
      gaps: [
        { unconfirmedFrom: "2026-03-05", unconfirmedBefore: "2026-03-31" },
      ],
    });
    expect(bounds(t)).toEqual({ from: "2026-03-05", before: "2026-03-31" });
    expect(bounds(create(TickerSchema, { txnDays: ["2026-03-05"] }))).toEqual({
      from: "2026-03-05",
      before: "2026-03-06",
    });
  });

  it("cuts at a first's day and the day after a last, and bounds the cut", () => {
    const first = { assertedOn: "2026-03-05", edge: Edge.FIRST };
    const last = { assertedOn: "2026-03-09", edge: Edge.LAST };
    expect(cutDay(create(AssertionItemSchema, first))).toBe("2026-03-05");
    expect(cutDay(create(AssertionItemSchema, last))).toBe("2026-03-10");
    expect(
      cutDay(create(AssertionItemSchema, { assertedOn: "2026-03-05" })),
    ).toBeUndefined();
    const t = create(TickerSchema, {
      txnDays: ["2026-03-01"],
      assertions: [create(AssertionItemSchema, last)],
    });
    expect(bounds(t)).toEqual({ from: "2026-03-01", before: "2026-03-10" });
  });
});
