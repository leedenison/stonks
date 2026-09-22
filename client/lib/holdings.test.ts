import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  DescriptionSchema,
  GroupHoldingSchema,
  InstrumentHoldingSchema,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  Broker,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { holdingRows } from "./holdings";

function ident(type: IdentifierType, value: string) {
  return create(IdentifierSchema, { type, value });
}

function instrument(
  instrumentId: string,
  assetClass: AssetClass,
  identifiers: ReturnType<typeof ident>[],
  quantity = "1",
) {
  return create(InstrumentHoldingSchema, {
    instrumentId,
    assetClass,
    identifiers,
    quantity,
  });
}

function group(
  groupId: string,
  assetClasses: AssetClass[],
  identifiers: ReturnType<typeof ident>[],
  descriptions: { broker: Broker; text: string }[],
  quantity = "1",
) {
  return create(GroupHoldingSchema, {
    groupId,
    assetClasses,
    identifiers,
    descriptions: descriptions.map((d) => create(DescriptionSchema, d)),
    quantity,
  });
}

function rows(
  instruments: ReturnType<typeof instrument>[],
  groups: ReturnType<typeof group>[] = [],
) {
  return holdingRows(
    create(ListHoldingsResponseSchema, { instruments, groups }),
  );
}

describe("holdingRows", () => {
  it("names cash by its currency wherever that identifier sits", () => {
    const gbp = instrument("i-gbp", AssetClass.CASH, [
      ident(IdentifierType.SEDOL, "0000000"),
      ident(IdentifierType.CURRENCY, "GBP"),
    ]);
    expect(rows([gbp])[0].label).toBe("GBP");
  });

  it("prefers a ticker to the registry codes", () => {
    const acme = instrument("i-acme", AssetClass.SECURITY, [
      ident(IdentifierType.CUSIP, "037833100"),
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.MIC_TICKER, "ACME"),
    ]);
    expect(rows([acme])[0].label).toBe("ACME");
  });

  it("prefers the registry codes by how widely they are quoted", () => {
    const codes = instrument("i-codes", AssetClass.STOCK, [
      ident(IdentifierType.BROKER_ID, "624291205"),
      ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
      ident(IdentifierType.SEDOL, "2046251"),
      ident(IdentifierType.CUSIP, "037833100"),
      ident(IdentifierType.ISIN, "US0378331005"),
    ]);
    expect(rows([codes])[0].label).toBe("US0378331005");
  });

  it("names an option by its OCC symbol and a future likewise", () => {
    const ids = [
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.OCC, "ACME  260116C00150000"),
    ];
    for (const c of [AssetClass.OPTION, AssetClass.FUTURE]) {
      expect(rows([instrument("i-d", c, ids)])[0].label).toBe(
        "ACME  260116C00150000",
      );
    }
  });

  it("ignores a type its class does not prefer", () => {
    const odd = instrument("i-odd", AssetClass.SECURITY, [
      ident(IdentifierType.CURRENCY, "USD"),
    ]);
    expect(rows([odd])[0].label).toBe("i-odd");
  });

  it("falls back through the order, then to the instrument id", () => {
    const bare = instrument("i-bare", AssetClass.EQUITY, [
      ident(IdentifierType.ISIN, "GB0002634946"),
    ]);
    const unnamed = instrument("i-unnamed", AssetClass.EQUITY, []);
    expect(rows([bare])[0].label).toBe("GB0002634946");
    expect(rows([unnamed])[0].label).toBe("i-unnamed");
  });

  it("names a group by a description a broker gave the line", () => {
    const g = group(
      "g-1",
      [AssetClass.SECURITY],
      [ident(IdentifierType.ISIN, "US0000000001")],
      [
        { broker: Broker.IBKR, text: "ACME CORP" },
        { broker: Broker.SCHWAB, text: "ACME CORPORATION" },
      ],
    );
    expect(rows([], [g])[0].label).toBe("ACME CORP");
  });

  it("falls back to a group's identifiers, then to the group id", () => {
    const named = group(
      "g-2",
      [],
      [ident(IdentifierType.MIC_TICKER, "ACME")],
      [],
    );
    const unnamed = group("g-3", [], [], []);
    expect(rows([], [named])[0].label).toBe("ACME");
    expect(rows([], [unnamed])[0].label).toBe("g-3");
  });

  it("carries every class a group states", () => {
    const g = group(
      "g-4",
      [AssetClass.SECURITY, AssetClass.EQUITY],
      [],
      [{ broker: Broker.IBKR, text: "ACME CORP" }],
    );
    expect(rows([], [g])[0].classes).toEqual([
      AssetClass.SECURITY,
      AssetClass.EQUITY,
    ]);
  });

  it("puts cash first and the rest by label, of either kind", () => {
    const gbp = instrument("i-gbp", AssetClass.CASH, [
      ident(IdentifierType.CURRENCY, "GBP"),
    ]);
    const vanguard = instrument("i-vusa", AssetClass.SECURITY, [
      ident(IdentifierType.MIC_TICKER, "VUSA"),
    ]);
    const bae = group(
      "g-bae",
      [AssetClass.SECURITY],
      [],
      [{ broker: Broker.FIDELITY_UK, text: "BAE SYSTEMS" }],
    );
    expect(rows([vanguard, gbp], [bae]).map((r) => r.id)).toEqual([
      "i-gbp",
      "g-bae",
      "i-vusa",
    ]);
  });
});
