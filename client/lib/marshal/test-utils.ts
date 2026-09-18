import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { create } from "@bufbuild/protobuf";
import {
  AssetClass,
  type Identifier,
  IdentifierSchema,
  IdentifierType,
  type StatedKey,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { type Row, RowSchema } from "@/gen/statement/v1/statement_pb";

// Test support. Every fixture under testdata/ is modelled on a real export
// with its account, reference and free-text identifiers replaced.

// fixture reads a file under testdata/. The path is taken from the working
// directory, the client root under vitest, because import.meta.url of a
// module imported from another directory is root-relative there.
export function fixture(name: string): string {
  return readFileSync(
    resolve(process.cwd(), "lib/marshal/testdata", name),
    "utf8",
  );
}

export function cash(currency: string): StatedKey {
  return create(StatedKeySchema, {
    identifiers: [
      create(IdentifierSchema, {
        type: IdentifierType.CURRENCY,
        value: currency,
      }),
    ],
    assetClass: AssetClass.CASH,
    currency,
  });
}

export function security(
  description: string,
  assetClass: AssetClass,
  identifiers: Identifier[],
  currency?: string,
): StatedKey {
  return create(StatedKeySchema, {
    identifiers,
    assetClass,
    currency,
    description,
  });
}

export function hint(symbol: string): Identifier {
  return create(IdentifierSchema, {
    type: IdentifierType.MIC_TICKER,
    value: symbol,
  });
}

export function row(
  key: StatedKey,
  orderDate: string,
  settlementDate: string,
  quantity: string,
): Row {
  return create(RowSchema, {
    key,
    orderDate,
    settlementDate,
    asAt: orderDate,
    quantity,
  });
}
