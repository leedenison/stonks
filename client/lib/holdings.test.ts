import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  type InstrumentHolding,
  InstrumentHoldingSchema,
} from "@/gen/holding/v1/holding_pb";
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
): InstrumentHolding {
  return create(InstrumentHoldingSchema, {
    instrumentId,
    assetClass,
    identifiers,
  });
}

const gbp = holding("i-gbp", AssetClass.CASH, [
  ident(IdentifierType.SEDOL, "0000000"),
  ident(IdentifierType.CURRENCY, "GBP"),
]);
const acme = holding("i-acme", AssetClass.SECURITY, [
  ident(IdentifierType.CUSIP, "037833100"),
  ident(IdentifierType.ISIN, "US0378331005"),
  ident(IdentifierType.MIC_TICKER, "ACME"),
]);
const bare = holding("i-bare", AssetClass.EQUITY, [
  ident(IdentifierType.ISIN, "GB0002634946"),
]);
const unnamed = holding("i-unnamed", AssetClass.EQUITY, []);

describe("holdingLabel", () => {
  it("names cash by its currency wherever that identifier sits", () => {
    expect(holdingLabel(gbp)).toBe("GBP");
  });

  it("prefers a ticker to the registry codes", () => {
    expect(holdingLabel(acme)).toBe("ACME");
  });

  it("takes a ticker stated without its venue", () => {
    const hint = holding("i-hint", AssetClass.SECURITY, [
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.MIC_TICKER, "ACME"),
    ]);
    expect(holdingLabel(hint)).toBe("ACME");
  });

  it("prefers the registry codes by how widely they are quoted", () => {
    const codes = holding("i-codes", AssetClass.STOCK, [
      ident(IdentifierType.BROKER_ID, "624291205"),
      ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
      ident(IdentifierType.SEDOL, "2046251"),
      ident(IdentifierType.CUSIP, "037833100"),
      ident(IdentifierType.ISIN, "US0378331005"),
    ]);
    expect(holdingLabel(codes)).toBe("US0378331005");
  });

  it("names an option by its OCC symbol and a future likewise", () => {
    const ids = [
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.OCC, "ACME  260116C00150000"),
    ];
    expect(holdingLabel(holding("i-opt", AssetClass.OPTION, ids))).toBe(
      "ACME  260116C00150000",
    );
    expect(holdingLabel(holding("i-fut", AssetClass.FUTURE, ids))).toBe(
      "ACME  260116C00150000",
    );
  });

  it("ignores a type its class does not prefer", () => {
    const odd = holding("i-odd", AssetClass.SECURITY, [
      ident(IdentifierType.CURRENCY, "USD"),
    ]);
    expect(holdingLabel(odd)).toBe("i-odd");
  });

  it("falls back through the order, then to the instrument id", () => {
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
