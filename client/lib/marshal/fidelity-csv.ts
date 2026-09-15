// The Fidelity (UK) activity CSV: a preamble stating the timeframe, a blank
// line, then a header and one line per transaction across every account.
// Every amount is in GBP. A line whose investment is "Cash" is a cash leg of
// its amount whatever its type; any other line is a security leg of its
// quantity, negated for a sell. The cash side of a trade is a line of its
// own. Every row is as at its order date.
//
// A cancelled line is not emitted. A line awaiting completion is not emitted
// either, and nor is any line ordered on or after the earliest such line,
// and the claimed period ends there: a later export supplies them all, where
// a period claiming the days they fall on would have deleted them for good.
// A zero charge awaiting completion is left out of that: real exports carry
// zero dealing fees and levies that stay pending for months, and a line that
// moves nothing has nothing to supply later.

import { parse } from "csv-parse/sync";
import { AssetClass, Broker, IdentifierType } from "@/gen/type/v1/type_pb";
import type { Row, Statement } from "@/gen/statement/v1/statement_pb";
import { cashKey, ident, leg, securityKey, statement } from "./build";
import { isZero, negate } from "./decimal";
import { iso, minDate, monthNumber, prevDay } from "./date";
import { MarshalError } from "./error";

const GBP = "GBP";

const BUYS = new Set([
  "Buy",
  "Buy From Dividend",
  "Buy From Rebate",
  "Reinvestment From Income",
]);
const SELLS = new Set(["Sell", "Sell For Switch"]);

const TIMEFRAME = /^(\d{2})\/(\d{2})\/(\d{4})-(\d{2})\/(\d{2})\/(\d{4})$/;
const DAY = /^(\d{2}) ([A-Z][a-z]{2}) (\d{4})$/;
const SYMBOL = /\(([A-Z0-9.]+)\)\s*$/;

interface Parsed {
  record: string[];
  info: { lines: number };
}

function isParsed(v: unknown): v is Parsed {
  return (
    typeof v === "object" &&
    v !== null &&
    Array.isArray((v as { record?: unknown }).record) &&
    typeof (v as { info?: { lines?: unknown } }).info?.lines === "number"
  );
}

function ukDate(s: string, line: number): string {
  const m = DAY.exec(s);
  const month = m && monthNumber(m[2]);
  if (!m || !month) throw new MarshalError(`malformed date ${s}`, line);
  return iso(m[3], month, m[1]);
}

interface Line {
  line: number;
  order: string;
  settlement?: string;
  type: string;
  investment: string;
  amount: string;
  quantity: string;
}

function marshal(text: string): Statement {
  const parsed: unknown = parse(text, {
    bom: true,
    relax_column_count: true,
    info: true,
  });
  if (!Array.isArray(parsed) || !parsed.every(isParsed)) {
    throw new MarshalError("not a Fidelity activity export");
  }
  let period: { from: string; to: string } | undefined;
  let columns: string[] | undefined;
  const lines: Line[] = [];

  for (const { record, info } of parsed) {
    const line = info.lines;
    if (!columns) {
      if (record[0] === "Timeframe") {
        const m = TIMEFRAME.exec(record[1] ?? "");
        if (!m)
          throw new MarshalError(`malformed timeframe ${record[1]}`, line);
        period = { from: iso(m[3], m[2], m[1]), to: iso(m[6], m[5], m[4]) };
      } else if (record[0] === "Order date") {
        columns = record;
      }
      continue;
    }
    const col = (name: string) => {
      const i = columns?.indexOf(name) ?? -1;
      const v = record[i];
      if (v === undefined) throw new MarshalError(`missing ${name}`, line);
      return v;
    };
    if (col("Status") !== "Completed") continue;
    const completion = col("Completion date");
    lines.push({
      line,
      order: ukDate(col("Order date"), line),
      settlement:
        completion === "Pending" ? undefined : ukDate(completion, line),
      type: col("Transaction type"),
      investment: col("Investments"),
      amount: col("Amount"),
      quantity: col("Quantity"),
    });
  }
  if (!columns) throw new MarshalError("not a Fidelity activity export");

  const pending = lines
    .filter(
      (l) =>
        l.settlement === undefined && !(isZero(l.amount) && isZero(l.quantity)),
    )
    .map((l) => l.order);
  const cutoff = pending.length ? pending.reduce(minDate) : undefined;
  if (cutoff !== undefined && period) {
    period =
      cutoff > period.from
        ? { from: period.from, to: prevDay(cutoff) }
        : undefined;
  }

  const rows: Row[] = [];
  for (const l of lines) {
    if (
      l.settlement === undefined ||
      (cutoff !== undefined && l.order >= cutoff)
    )
      continue;
    if (l.investment === "Cash") {
      rows.push(leg(cashKey(GBP), l.order, l.settlement, l.amount, GBP));
      continue;
    }
    if (!BUYS.has(l.type) && !SELLS.has(l.type)) {
      throw new MarshalError(
        `unknown transaction type ${l.type}`,
        l.line,
        l.type,
      );
    }
    const symbol = SYMBOL.exec(l.investment)?.[1];
    const key = securityKey({
      description: l.investment,
      assetClass: AssetClass.SECURITY,
      currency: GBP,
      identifiers: symbol ? [ident(IdentifierType.MIC_TICKER, symbol)] : [],
    });
    rows.push(
      leg(
        key,
        l.order,
        l.settlement,
        SELLS.has(l.type) ? negate(l.quantity) : l.quantity,
        GBP,
      ),
    );
  }

  return statement(Broker.FIDELITY, rows, [], period);
}

// The export date is not needed: every row is as at its order date.
export const fidelityCsv = { marshal };
