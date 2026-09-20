import { create } from "@bufbuild/protobuf";
import {
  type Statement,
  StatementSchema,
} from "@/gen/statement/v1/statement_pb";
import { nextDay, prevDay } from "@/lib/marshal/date";

// Period is an inclusive range of order dates, each an ISO date.
export type Period = { from: string; to: string };

// period is the period a statement is uploaded over: chosen where one has
// been, and otherwise the one the export states with its end made inclusive.
// outside counts the rows the period excludes, and valid is false while an
// end is blank or the period is reversed.
export function period(
  statement: Statement,
  chosen?: Period,
): Period & { outside: number; valid: boolean } {
  const from = chosen?.from ?? statement.orderFrom;
  const to = chosen?.to ?? prevDay(statement.orderBefore);
  const outside = statement.rows.filter(
    (r) => r.orderDate < from || r.orderDate > to,
  ).length;
  return { from, to, outside, valid: from !== "" && to !== "" && from <= to };
}

// claim restates statement over p, whose end is inclusive where the
// statement's is not.
export function claim(statement: Statement, p: Period): Statement {
  return create(StatementSchema, {
    ...statement,
    orderFrom: p.from,
    orderBefore: nextDay(p.to),
  });
}
