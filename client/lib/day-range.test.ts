import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { PeriodSchema } from "@/gen/type/v1/type_pb";
import { dayRange, periodOf } from "./day-range";

describe("dayRange", () => {
  it("includes the day before the period's end", () => {
    const p = create(PeriodSchema, {
      from: "2024-02-01",
      before: "2024-03-01",
    });
    expect(dayRange(p)).toEqual({ from: "2024-02-01", to: "2024-02-29" });
  });

  it("leaves an open bound empty", () => {
    expect(dayRange(create(PeriodSchema, { from: "2024-02-01" }))).toEqual({
      from: "2024-02-01",
      to: "",
    });
    expect(dayRange(undefined)).toEqual({ from: "", to: "" });
  });
});

describe("periodOf", () => {
  it("ends the period the day after the range", () => {
    expect(periodOf({ from: "2024-12-01", to: "2024-12-31" })).toEqual(
      create(PeriodSchema, { from: "2024-12-01", before: "2025-01-01" }),
    );
  });
});
