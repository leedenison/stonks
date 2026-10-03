import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { nameOf, pick, preferred } from "./identifiers";

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

describe("nameOf", () => {
  it("names cash by its currency wherever that identifier sits", () => {
    const ids = [
      ident(IdentifierType.SEDOL, "0000000"),
      ident(IdentifierType.CURRENCY, "GBP"),
    ];
    expect(nameOf(AssetClass.CASH, ids)?.value).toBe("GBP");
  });

  it("prefers a ticker to the registry codes, the first venue listed", () => {
    const ids = [
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.MIC_TICKER, "ACME", "XNAS"),
      ident(IdentifierType.MIC_TICKER, "ACME.", "XNYS"),
    ];
    expect(nameOf(AssetClass.STOCK, ids)?.domain).toBe("XNAS");
  });

  it("names an option by its OCC symbol and a future likewise", () => {
    const ids = [
      ident(IdentifierType.ISIN, "US0378331005"),
      ident(IdentifierType.OCC, "ACME  260116C00150000"),
    ];
    for (const c of [AssetClass.OPTION, AssetClass.FUTURE]) {
      expect(nameOf(c, ids)?.type).toBe(IdentifierType.OCC);
    }
  });

  it("ignores a type its class does not prefer, and names nothing without one", () => {
    expect(
      nameOf(AssetClass.SECURITY, [ident(IdentifierType.CURRENCY, "USD")]),
    ).toBeUndefined();
    expect(nameOf(AssetClass.EQUITY, [])).toBeUndefined();
  });
});

describe("pick", () => {
  it("walks the order given", () => {
    const ids = [
      ident(IdentifierType.BROKER_ID, "1", "ibkr"),
      ident(IdentifierType.ISIN, "US0378331005"),
    ];
    expect(pick(preferred, ids)?.type).toBe(IdentifierType.ISIN);
    expect(pick([IdentifierType.BROKER_ID], ids)?.value).toBe("1");
    expect(pick([IdentifierType.SEDOL], ids)).toBeUndefined();
  });
});
