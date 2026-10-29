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
    ({ period: p }) =>
      (p?.from === undefined || p.from <= day) &&
      (p?.before === undefined || day < p.before),
  );
}

// cutDays returns the days at whose start an assertion's edges cut the
// timeline: the bounds of its period that are edges.
export function cutDays(a: AssertionItem): string[] {
  const out: string[] = [];
  if (a.edgeFrom && a.period?.from !== undefined) out.push(a.period.from);
  if (a.edgeBefore && a.period?.before !== undefined) out.push(a.period.before);
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
  const periods = [
    ...t.assertionCoverage.map((p) => p.period),
    ...t.assertions.map((a) => a.period),
    ...t.intervals.map((iv) => iv.period),
    ...t.gaps.map((g) => g.period),
  ];
  for (const p of periods) {
    if (p?.from !== undefined) days.push(p.from);
    if (p?.before !== undefined) days.push(p.before);
  }
  if (days.length === 0) return undefined;
  days.sort();
  const last = days[days.length - 1];
  const before = last === days[0] ? nextDay(last) : last;
  return { from: days[0], before };
}
