// The Schwab transaction history, as the JSON export or the CSV export. Both
// carry the same lines; the JSON states the period and the CSV does not. A
// line stating a posted date "as of" an effective date is ordered on the
// effective date and settled on the posted date. Every amount is in USD, and
// a symbol is carried as a hint with no venue.
//
// A quantity is restated to the units held after every split up to the
// export while the price stays as traded, and the split is still stated as a
// line of its own. Every row is therefore as at the date the export was
// taken, which neither format states and the caller supplies.

import { parse } from "csv-parse/sync";
import { AssetClass, Broker, IdentifierType } from "@/gen/type/v1/type_pb";
import type {
  Row,
  StatedSplit,
  Statement,
} from "@/gen/statement/v1/statement_pb";
import {
  cashKey,
  ident,
  leg,
  securityKey,
  split,
  trade,
  statement,
} from "./build";
import { negate } from "./decimal";
import { iso } from "./date";
import { MarshalError } from "./error";
import type { Marshaller } from "./marshal";
import { mediaType } from "./media";

const USD = "USD";

// The types a browser reports for the JSON and the CSV export. A CSV is
// reported as a spreadsheet on a system where a spreadsheet owns the
// extension.
const TYPES = new Set([
  "application/json",
  "text/csv",
  "application/vnd.ms-excel",
]);

const COLUMNS = [
  "Date",
  "Action",
  "Symbol",
  "Description",
  "Quantity",
  "Price",
  "Fees & Comm",
  "Amount",
];

interface Line {
  line: number;
  date: string;
  action: string;
  symbol: string;
  description: string;
  quantity: string;
  fees: string;
  amount: string;
}

interface Export {
  period?: { from: string; to: string };
  lines: Line[];
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function field(
  rec: Record<string, unknown>,
  name: string,
  line: number,
): string {
  const v = rec[name];
  if (typeof v !== "string") throw new MarshalError(`missing ${name}`, line);
  return v;
}

function toLine(rec: Record<string, unknown>, line: number): Line {
  return {
    line,
    date: field(rec, "Date", line),
    action: field(rec, "Action", line),
    symbol: field(rec, "Symbol", line),
    description: field(rec, "Description", line),
    quantity: field(rec, "Quantity", line),
    fees: field(rec, "Fees & Comm", line),
    amount: field(rec, "Amount", line),
  };
}

const US_DATE = /^(\d{2})\/(\d{2})\/(\d{4})$/;

function usDate(s: string, line: number): string {
  const m = US_DATE.exec(s);
  if (!m) throw new MarshalError(`malformed date ${s}`, line);
  return iso(m[3], m[1], m[2]);
}

function readJson(text: string): Export {
  const doc: unknown = JSON.parse(text);
  if (!isRecord(doc) || !Array.isArray(doc["BrokerageTransactions"])) {
    throw new MarshalError("not a Schwab transaction history");
  }
  const lines = doc["BrokerageTransactions"].map((rec: unknown, i) => {
    if (!isRecord(rec)) throw new MarshalError("malformed transaction", i + 1);
    return toLine(rec, i + 1);
  });
  return {
    period: {
      from: usDate(field(doc, "FromDate", 0), 0),
      to: usDate(field(doc, "ToDate", 0), 0),
    },
    lines,
  };
}

function readCsv(text: string): Export {
  const records: unknown = parse(text, { bom: true, columns: true });
  if (!Array.isArray(records))
    throw new MarshalError("not a Schwab transaction history");
  return {
    lines: records.map((rec: unknown, i) => {
      if (!isRecord(rec))
        throw new MarshalError("malformed transaction", i + 2);
      return toLine(rec, i + 2);
    }),
  };
}

const CASH_ACTIONS = new Set([
  "Cash Dividend",
  "Qualified Dividend",
  "Qual Div Reinvest",
  "Credit Interest",
  "Cash Merger",
  "Wire Sent",
  "Journal",
  "Misc Cash Entry",
  "Service Fee",
]);

function marshal(text: string, exportedOn: string): Statement {
  const { period, lines } = text.trimStart().startsWith("{")
    ? readJson(text)
    : readCsv(text);
  const rows: Row[] = [];
  const splits: StatedSplit[] = [];

  for (const l of lines) {
    const [posted, asOf] = l.date.split(" as of ");
    const settlement = usDate(posted, l.line);
    const order = asOf === undefined ? settlement : usDate(asOf, l.line);
    const key = () => {
      if (l.symbol === "")
        throw new MarshalError(`${l.action} without a symbol`, l.line);
      return securityKey({
        description: l.description,
        assetClass: AssetClass.SECURITY,
        currency: USD,
        identifiers: [ident(IdentifierType.MIC_TICKER, l.symbol)],
      });
    };
    const bought = (units: string) =>
      trade({
        key: key(),
        orderDate: order,
        settlementDate: settlement,
        units,
        net: l.amount,
        fee: l.fees === "" ? "0" : l.fees,
        tax: "0",
        settlementCurrency: USD,
        asAt: exportedOn,
      });

    switch (l.action) {
      case "Buy":
      case "Reinvest Shares":
        rows.push(...bought(l.quantity));
        break;
      case "Sell":
        rows.push(...bought(negate(l.quantity)));
        break;
      case "Cash Merger Adj":
        rows.push(leg(key(), order, settlement, l.quantity, exportedOn));
        break;
      case "Stock Split":
        splits.push(split(key(), order, l.quantity));
        break;
      default:
        if (!CASH_ACTIONS.has(l.action)) {
          throw new MarshalError(
            `unknown action ${l.action}`,
            l.line,
            l.action,
          );
        }
        rows.push(leg(cashKey(USD), order, settlement, l.amount, exportedOn));
    }
  }

  return statement(Broker.SCHWAB, rows, splits, period);
}

// Neither export states an account number, so after the type recognition
// rests on the format alone: the JSON's period and transaction list, or the
// CSV's header.
function recognise(text: string, type: string): boolean {
  if (!TYPES.has(mediaType(type))) return false;
  const body = text.replace(/^\uFEFF/, "").trimStart();
  if (body.startsWith("{")) {
    try {
      const doc: unknown = JSON.parse(body);
      return (
        isRecord(doc) &&
        typeof doc["FromDate"] === "string" &&
        typeof doc["ToDate"] === "string" &&
        Array.isArray(doc["BrokerageTransactions"])
      );
    } catch {
      return false;
    }
  }
  const header = body.split(/\r?\n/, 1)[0];
  return header === COLUMNS.map((c) => `"${c}"`).join(",");
}

export const schwab: Marshaller = { marshal, recognise };
