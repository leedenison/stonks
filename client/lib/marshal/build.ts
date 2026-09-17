// Builders for the messages of the neutral format, shared by every
// marshaller. See proto/statement/v1/statement.proto for the format.

import { create } from "@bufbuild/protobuf";
import {
  AssetClass,
  type Broker,
  type Identifier,
  IdentifierSchema,
  IdentifierType,
  type StatedKey,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import {
  type Row,
  RowSchema,
  SplitRatioSchema,
  type StatedSplit,
  StatedSplitSchema,
  type Statement,
  StatementSchema,
} from "@/gen/statement/v1/statement_pb";
import { add, isZero, negate, normalise } from "./decimal";
import { maxDate, minDate, nextDay } from "./date";

export function ident(
  type: IdentifierType,
  value: string,
  domain?: string,
): Identifier {
  return create(IdentifierSchema, { type, value, domain });
}

// cashKey names money in one currency: the currency's identifier, class CASH
// and the currency.
export function cashKey(currency: string): StatedKey {
  return create(StatedKeySchema, {
    identifiers: [ident(IdentifierType.CURRENCY, currency)],
    assetClass: AssetClass.CASH,
    currency,
  });
}

export function securityKey(key: {
  description: string;
  assetClass: AssetClass;
  currency?: string;
  identifiers: Identifier[];
}): StatedKey {
  return create(StatedKeySchema, key);
}

// leg builds one row, stated as at its order date unless asAt says otherwise.
export function leg(
  key: StatedKey,
  orderDate: string,
  settlementDate: string,
  quantity: string,
  asAt?: string,
): Row {
  return create(RowSchema, {
    key,
    orderDate,
    settlementDate,
    asAt: asAt ?? orderDate,
    quantity: normalise(quantity),
  });
}

// trade builds the legs of one export line carrying a trade: the security,
// the gross consideration, and a fee and a tax leg only when nonzero, so the
// cash legs sum to the net cash the export states.
export function trade(t: {
  key: StatedKey;
  orderDate: string;
  settlementDate: string;
  units: string;
  net: string;
  fee: string;
  tax: string;
  settlementCurrency: string;
  asAt?: string;
}): Row[] {
  const cash = cashKey(t.settlementCurrency);
  const at = (key: StatedKey, quantity: string) =>
    leg(key, t.orderDate, t.settlementDate, quantity, t.asAt);
  const rows = [at(t.key, t.units), at(cash, add(add(t.net, t.fee), t.tax))];
  for (const charge of [t.fee, t.tax]) {
    if (!isZero(charge)) rows.push(at(cash, negate(charge)));
  }
  return rows;
}

export function split(
  key: StatedKey,
  effectiveDate: string,
  quantity: string,
  ratio?: { from: string; to: string },
): StatedSplit {
  return create(StatedSplitSchema, {
    key,
    effectiveDate,
    quantity: normalise(quantity),
    ratio: ratio && create(SplitRatioSchema, ratio),
  });
}

// statement assembles the message. The period is the one the export states,
// given as its first and last order dates inclusive, and is otherwise derived
// from the rows. A row outside a stated period is kept as stated.
export function statement(
  broker: Broker,
  rows: Row[],
  splits: StatedSplit[],
  period?: { from: string; to: string },
): Statement {
  let span = period;
  if (!span) {
    for (const row of rows) {
      span = span
        ? {
            from: minDate(span.from, row.orderDate),
            to: maxDate(span.to, row.orderDate),
          }
        : { from: row.orderDate, to: row.orderDate };
    }
  }
  return create(StatementSchema, {
    broker,
    orderFrom: span?.from ?? "",
    orderBefore: span ? nextDay(span.to) : "",
    rows,
    splits,
  });
}
