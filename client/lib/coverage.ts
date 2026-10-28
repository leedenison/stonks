import type { Interval, Ticker } from "@/gen/admin/coverage/v1/coverage_pb";
import type { AssertionItem } from "@/gen/admin/run/v1/run_pb";
import type { Identifier } from "@/gen/type/v1/type_pb";
import { nextDay } from "@/lib/marshal/date";

// dayNumber counts the days from 1970-01-01 to a YYYY-MM-DD date, so dates
// subtract and compare as numbers.
export function dayNumber(date: string): number {
  const [y, m, d] = date.split("-").map(Number);
  return Date.UTC(y, m - 1, d) / 86_400_000;
}

// intervalAt returns the interval of ticker validity that holds day, or
// undefined when none does.
export function intervalAt(
  intervals: Interval[],
  day: string,
): Interval | undefined {
  return intervals.find(
    (iv) =>
      (iv.validFrom === undefined || iv.validFrom <= day) &&
      (iv.validBefore === undefined || day < iv.validBefore),
  );
}

// cutDays returns the days at whose start an assertion's edges cut the
// timeline: the bounds of its period that are edges.
export function cutDays(a: AssertionItem): string[] {
  const out: string[] = [];
  if (a.edgeFrom) out.push(a.assertedFrom);
  if (a.edgeBefore) out.push(a.assertedBefore);
  return out;
}

// instrName returns the identifier naming instrument id, or undefined
// when the ticker gives only the id.
export function instrName(t: Ticker, id: string): Identifier | undefined {
  return t.instrs.find((n) => n.id === id)?.name;
}

// bounds returns the day range the timeline draws, or undefined when the
// ticker has no dated fact. It ignores an open bound of an interval.
export function bounds(
  t: Ticker,
): { from: string; before: string } | undefined {
  const days: string[] = [...t.txnDays];
  for (const p of t.assertionCoverage)
    days.push(p.coveredFrom, p.coveredBefore);
  for (const a of t.assertions) days.push(a.assertedFrom, a.assertedBefore);
  for (const iv of t.intervals) {
    if (iv.validFrom !== undefined) days.push(iv.validFrom);
    if (iv.validBefore !== undefined) days.push(iv.validBefore);
  }
  for (const g of t.gaps) days.push(g.unconfirmedFrom, g.unconfirmedBefore);
  if (days.length === 0) return undefined;
  days.sort();
  const last = days[days.length - 1];
  const before = last === days[0] ? nextDay(last) : last;
  return { from: days[0], before };
}
