import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  IntervalSchema,
  TickerSchema,
  Validity,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { bounds, dayNumber, intervalAt } from "./coverage";

describe("coverage", () => {
  it("counts days and finds the interval holding a day", () => {
    expect(dayNumber("1970-01-02")).toBe(1);
    expect(dayNumber("2026-03-10") - dayNumber("2026-03-01")).toBe(9);
    const intervals = [
      create(IntervalSchema, {
        validBefore: "2026-03-10",
        instrumentId: "i1",
        validity: Validity.PROVISIONAL,
      }),
      create(IntervalSchema, {
        validFrom: "2026-03-10",
        validBefore: "2026-03-21",
        instrumentId: "i1",
        validity: Validity.CONFIRMED,
      }),
    ];
    expect(intervalAt(intervals, "2026-03-01")?.validity).toBe(
      Validity.PROVISIONAL,
    );
    expect(intervalAt(intervals, "2026-03-10")?.validity).toBe(
      Validity.CONFIRMED,
    );
    expect(intervalAt(intervals, "2026-03-20")?.validity).toBe(
      Validity.CONFIRMED,
    );
    expect(intervalAt(intervals, "2026-03-21")).toBeUndefined();
  });

  it("bounds the timeline by every dated fact, leaving open bounds out", () => {
    expect(bounds(create(TickerSchema, {}))).toBeUndefined();
    const t = create(TickerSchema, {
      transactionDays: ["2026-03-05"],
      intervals: [
        create(IntervalSchema, { validFrom: "2026-03-10", instrumentId: "i1" }),
      ],
      gaps: [
        { unconfirmedFrom: "2026-03-05", unconfirmedBefore: "2026-03-31" },
      ],
    });
    expect(bounds(t)).toEqual({ from: "2026-03-05", before: "2026-03-31" });
    expect(
      bounds(create(TickerSchema, { transactionDays: ["2026-03-05"] })),
    ).toEqual({
      from: "2026-03-05",
      before: "2026-03-06",
    });
  });
});
