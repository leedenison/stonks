import { create } from "@bufbuild/protobuf";
import { type Stmt, StmtSchema } from "@/gen/stmt/v1/stmt_pb";
import { nextDay, prevDay } from "@/lib/marshal/date";

// Period is an inclusive range of order dates, each an ISO date.
export type Period = { from: string; to: string };

// period is the period covered by an upload: chosen where one has
// been, and otherwise the one the export states with its end made inclusive.
// outside counts the rows the period excludes, and valid is false while an
// end is blank or the period is reversed.
export function period(
  stmt: Stmt,
  chosen?: Period,
): Period & { outside: number; valid: boolean } {
  const from = chosen?.from ?? stmt.orderFrom;
  const to = chosen?.to ?? prevDay(stmt.orderBefore);
  const outside = stmt.rows.filter(
    (r) => r.orderDate < from || r.orderDate > to,
  ).length;
  return { from, to, outside, valid: from !== "" && to !== "" && from <= to };
}

// claim restates statement over p, whose end is inclusive where the
// statement's is not.
export function claim(stmt: Stmt, p: Period): Stmt {
  return create(StmtSchema, {
    ...stmt,
    orderFrom: p.from,
    orderBefore: nextDay(p.to),
  });
}
