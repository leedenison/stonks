import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  InstrumentSchema,
  ListingSchema,
  ListInstrumentsResponseSchema,
} from "@/gen/instrument/v1/instrument_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { instrumentRows } from "./instruments";

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

const acme = create(InstrumentSchema, {
  id: "i-acme",
  assetClass: AssetClass.STOCK,
  identifiers: [
    ident(IdentifierType.ISIN, "US0378331005"),
    ident(IdentifierType.CUSIP, "037833100"),
  ],
  listings: [
    create(ListingSchema, {
      id: "l-usd",
      currency: "USD",
      identifiers: [ident(IdentifierType.MIC_TICKER, "ACME", "XNAS")],
    }),
  ],
});
const sedolOnly = create(InstrumentSchema, {
  id: "i-sedol",
  assetClass: AssetClass.EQUITY,
  listings: [
    create(ListingSchema, {
      id: "l-gbp",
      currency: "GBP",
      identifiers: [ident(IdentifierType.SEDOL, "0263494")],
    }),
  ],
});
const gbp = create(InstrumentSchema, {
  id: "i-gbp",
  assetClass: AssetClass.CASH,
  identifiers: [ident(IdentifierType.CURRENCY, "GBP")],
});

function rows(instruments: (typeof acme)[]) {
  return instrumentRows(create(ListInstrumentsResponseSchema, { instruments }));
}

describe("instrumentRows", () => {
  it("names an instrument by a listing's ticker and keeps its codes beside it", () => {
    const [row] = rows([acme]);
    expect(row.label).toBe("ACME");
    expect(row.identifiers.map((i) => i.value)).toEqual([
      "US0378331005",
      "037833100",
    ]);
    expect(row.listings.map((l) => l.currency)).toEqual(["USD"]);
  });

  it("names an instrument by a listing grain code when nothing else names it", () => {
    expect(rows([sedolOnly])[0].label).toBe("0263494");
  });

  it("falls back to the instrument id", () => {
    const bare = create(InstrumentSchema, { id: "i-bare" });
    expect(rows([bare])[0].label).toBe("i-bare");
  });

  it("puts cash first and the rest by label", () => {
    expect(rows([sedolOnly, acme, gbp]).map((r) => r.id)).toEqual([
      "i-gbp",
      "i-sedol",
      "i-acme",
    ]);
  });
});
