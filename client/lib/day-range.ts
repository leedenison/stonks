import { create } from "@bufbuild/protobuf";
import { type Period, PeriodSchema } from "@/gen/type/v1/type_pb";
import { nextDay, prevDay } from "@/lib/marshal/date";

// A DayRange is a range of days that includes both ends, each YYYY-MM-DD.
// The API carries the same days as a half open Period.
export type DayRange = { from: string; to: string };

// dayRange returns the days of p. An open or absent bound is the empty
// string.
export function dayRange(p: Period | undefined): DayRange {
  return {
    from: p?.from ?? "",
    to: p?.before === undefined ? "" : prevDay(p.before),
  };
}

// periodOf returns the half open period that holds the days of r.
export function periodOf(r: DayRange): Period {
  return create(PeriodSchema, { from: r.from, before: nextDay(r.to) });
}
