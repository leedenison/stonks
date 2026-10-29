import { create } from "@bufbuild/protobuf";
import { type Stmt, StmtSchema } from "@/gen/stmt/v1/stmt_pb";
import { type DayRange, dayRange, periodOf } from "@/lib/day-range";

// period is the period covered by an upload: chosen where one has
// been, and otherwise the one the export states with its end made inclusive.
// outside counts the rows the period excludes, and valid is false while an
// end is blank or the period is reversed.
export function period(
  stmt: Stmt,
  chosen?: DayRange,
): DayRange & { outside: number; valid: boolean } {
  const { from, to } = chosen ?? dayRange(stmt.period);
  const outside = stmt.rows.filter(
    (r) => r.orderDate < from || r.orderDate > to,
  ).length;
  return { from, to, outside, valid: from !== "" && to !== "" && from <= to };
}

// claim restates statement over r, whose end is inclusive where the
// statement's is not.
export function claim(stmt: Stmt, r: DayRange): Stmt {
  return create(StmtSchema, { ...stmt, period: periodOf(r) });
}
