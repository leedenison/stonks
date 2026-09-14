import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { create } from "@bufbuild/protobuf";
import {
  AssetClass,
  type Identifier,
  IdentifierSchema,
  IdentifierType,
  type StatedKey,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import { type Row, RowSchema } from "@/gen/upload/v1/upload_pb";

// Test support. Every fixture under testdata/ is modelled on a real export
// with its account, reference and free-text identifiers replaced.

export function fixture(name: string): string {
  return readFileSync(
    fileURLToPath(new URL(`./testdata/${name}`, import.meta.url)),
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
  currency?: string,
): Row {
  return create(RowSchema, {
    key,
    orderDate,
    settlementDate,
    asAt: orderDate,
    quantity,
    currency,
  });
}
