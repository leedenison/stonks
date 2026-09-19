import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { type Holding, HoldingSchema } from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { holdingLabel, sortHoldings } from "./holdings";

function ident(type: IdentifierType, value: string) {
  return create(IdentifierSchema, { type, value });
}

function holding(
  instrumentId: string,
  assetClass: AssetClass,
  identifiers: ReturnType<typeof ident>[],
): Holding {
  return create(HoldingSchema, { instrumentId, assetClass, identifiers });
}

const gbp = holding("i-gbp", AssetClass.CASH, [
  ident(IdentifierType.BROKER_DESCRIPTION, "Pounds"),
  ident(IdentifierType.CURRENCY, "GBP"),
]);
const acme = holding("i-acme", AssetClass.SECURITY, [
  ident(IdentifierType.ISIN, "US0378331005"),
  ident(IdentifierType.BROKER_DESCRIPTION, "ACME CORP"),
]);
const bare = holding("i-bare", AssetClass.EQUITY, [
  ident(IdentifierType.ISIN, "GB0002634946"),
]);
const unnamed = holding("i-unnamed", AssetClass.EQUITY, []);

describe("holdingLabel", () => {
  it("names cash by its currency wherever that identifier sits", () => {
    expect(holdingLabel(gbp)).toBe("GBP");
  });

  it("names a security by its broker description", () => {
    expect(holdingLabel(acme)).toBe("ACME CORP");
  });

  it("falls back to the first identifier, then the instrument id", () => {
    expect(holdingLabel(bare)).toBe("GB0002634946");
    expect(holdingLabel(unnamed)).toBe("i-unnamed");
  });
});

describe("sortHoldings", () => {
  it("puts cash first and the rest by label, leaving the input alone", () => {
    const input = [bare, acme, gbp];
    const sorted = sortHoldings(input);
    expect(sorted.map((h) => h.instrumentId)).toEqual([
      "i-gbp",
      "i-acme",
      "i-bare",
    ]);
    expect(input.map((h) => h.instrumentId)).toEqual([
      "i-bare",
      "i-acme",
      "i-gbp",
    ]);
  });
});
