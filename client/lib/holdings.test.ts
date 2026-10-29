import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  GroupHoldingSchema,
  HoldingKeySchema,
  HoldingListingSchema,
  HoldingMark,
  InstrHoldingSchema,
  ListHoldingsResponseSchema,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  IdentifierSchema,
  IdentifierType,
  TickerRelation,
} from "@/gen/type/v1/type_pb";
import { hasDetail, holdingRows } from "./holdings";

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

// described builds the identifier a key states for one broker's description.
function described(broker: string, text: string) {
  return ident(IdentifierType.BROKER_DESCRIPTION, text, broker);
}

// instr builds a holding named by the first of identifiers, as the
// API names it, or unnamed where there is none.
function instr(
  instrId: string,
  assetClass: AssetClass,
  identifiers: ReturnType<typeof ident>[],
  quantity = "1",
) {
  return create(InstrHoldingSchema, {
    instrId,
    assetClass,
    name: identifiers[0],
    identifiers,
    quantity,
  });
}

function group(
  groupId: string,
  assetClasses: AssetClass[],
  identifiers: ReturnType<typeof ident>[],
  quantity = "1",
) {
  return create(GroupHoldingSchema, {
    groupId,
    assetClasses,
    identifiers,
    quantity,
  });
}

function rows(
  instrs: ReturnType<typeof instr>[],
  groups: ReturnType<typeof group>[] = [],
) {
  return holdingRows(create(ListHoldingsResponseSchema, { instrs, groups }));
}

describe("holdingRows", () => {
  it("names a holding by the name the API gives it, whatever its class", () => {
    const gbp = instr("i-gbp", AssetClass.CASH, [
      ident(IdentifierType.CURRENCY, "GBP"),
      ident(IdentifierType.SEDOL, "0000000"),
    ]);
    const option = instr("i-opt", AssetClass.OPTION, [
      ident(IdentifierType.OCC, "ACME  260116C00150000"),
      ident(IdentifierType.ISIN, "US0378331005"),
    ]);
    expect(rows([gbp])[0].label).toBe("GBP");
    expect(rows([option])[0].label).toBe("ACME  260116C00150000");
  });

  it("prefers the held listing's ticker to the name, at its venue", () => {
    const acme = create(InstrHoldingSchema, {
      instrId: "i-acme",
      assetClass: AssetClass.OPTION,
      name: ident(IdentifierType.OCC, "ACME  260116C00150000"),
      quantity: "2",
      listings: [
        create(HoldingListingSchema, {
          id: "l-usd",
          currency: "USD",
          venue: "Cboe",
          ticker: { market: "XCBO", value: "ACMETICK" },
          relation: TickerRelation.CURRENT,
        }),
      ],
      keys: [create(HoldingKeySchema, { listingId: "l-usd", quantity: "2" })],
    });
    const row = rows([acme])[0];
    expect(row.label).toBe("ACMETICK");
    expect(row.venue).toBe("Cboe");
  });

  it("marks a ticker that now names another instrument, and keeps a former one plain", () => {
    const held = (relation: TickerRelation, venue?: string) =>
      create(InstrHoldingSchema, {
        instrId: "i-old",
        assetClass: AssetClass.EQUITY,
        quantity: "5",
        listings: [
          create(HoldingListingSchema, {
            id: "l-usd",
            currency: "USD",
            venue,
            ticker: { market: "US", value: "OLDCO" },
            relation,
          }),
        ],
      });
    const defunct = rows([held(TickerRelation.DEFUNCT, "NYSE")])[0];
    expect(defunct.label).toBe("OLDCO (defunct)");
    expect(defunct.venue).toBe("NYSE");
    const former = rows([held(TickerRelation.FORMER)])[0];
    expect(former.label).toBe("OLDCO");
    expect(former.venue).toBe("US");
  });

  it("falls back to the instrument id without a name", () => {
    const unnamed = instr("i-unnamed", AssetClass.EQUITY, []);
    expect(rows([unnamed])[0].label).toBe("i-unnamed");
  });

  it("names a group by a description a broker gave the line before a code", () => {
    const g = group(
      "g-1",
      [AssetClass.SECURITY],
      [
        ident(IdentifierType.ISIN, "US0000000001"),
        described("ibkr", "ACME CORP"),
        described("schwab", "ACME CORPORATION"),
      ],
    );
    expect(rows([], [g])[0].label).toBe("ACME CORP");
  });

  it("names a group of an option by the symbol its keys state", () => {
    const g = group(
      "g-opt",
      [AssetClass.OPTION],
      [
        described("ibkr", "AMZN 240920P00135000 AMZN 20SEP24 135 P"),
        ident(IdentifierType.OPTION, "AMZN  240920P00135000"),
      ],
    );
    const row = rows([], [g])[0];
    expect(row.label).toBe("AMZN  240920P00135000");
    expect(row.descriptions).toEqual([
      "AMZN 240920P00135000 AMZN 20SEP24 135 P",
    ]);
  });

  it("falls back to a group's identifiers, then to the group id", () => {
    const named = group("g-2", [], [ident(IdentifierType.MIC_TICKER, "ACME")]);
    const unnamed = group("g-3", [], []);
    expect(rows([], [named])[0].label).toBe("ACME");
    expect(rows([], [unnamed])[0].label).toBe("g-3");
  });

  it("carries every class a group states", () => {
    const g = group(
      "g-4",
      [AssetClass.SECURITY, AssetClass.EQUITY],
      [described("ibkr", "ACME CORP")],
    );
    expect(rows([], [g])[0].classes).toEqual([
      AssetClass.SECURITY,
      AssetClass.EQUITY,
    ]);
  });

  it("shows the registry codes beside a resolved holding's name", () => {
    const acme = instr("i-acme", AssetClass.STOCK, [
      create(IdentifierSchema, {
        type: IdentifierType.MIC_TICKER,
        domain: "XNAS",
        value: "ACME",
      }),
      create(IdentifierSchema, {
        type: IdentifierType.MIC_TICKER,
        domain: "XNYS",
        value: "ACME",
      }),
      ident(IdentifierType.OPENFIGI_SHARE_CLASS, "BBG001S5N8V8"),
      ident(IdentifierType.CUSIP, "037833100"),
      ident(IdentifierType.ISIN, "US0378331005"),
      create(IdentifierSchema, {
        type: IdentifierType.BROKER_ID,
        domain: "ibkr",
        value: "265598",
      }),
    ]);
    const row = rows([acme])[0];
    expect(row.kind).toBe("instr");
    expect(row.label).toBe("ACME");
    expect(row.identifiers.map((i) => i.value)).toEqual([
      "BBG001S5N8V8",
      "037833100",
      "US0378331005",
    ]);
  });

  it("keeps a group's other descriptions apart from its name", () => {
    const g = group(
      "g-5",
      [AssetClass.SECURITY],
      [
        ident(IdentifierType.MIC_TICKER, "ACME"),
        ident(IdentifierType.ISIN, "US0000000001"),
        described("ibkr", "ACME CORP"),
        described("schwab", "ACME CORPORATION"),
      ],
    );
    const row = rows([], [g])[0];
    expect(row.kind).toBe("group");
    expect(row.label).toBe("ACME");
    expect(row.descriptions).toEqual(["ACME CORP", "ACME CORPORATION"]);
  });

  it("keeps only the registry codes of a group among its identifiers", () => {
    const g = group(
      "g-6",
      [],
      [
        ident(IdentifierType.ISIN, "US0000000001"),
        ident(IdentifierType.MIC_TICKER, "ACME"),
      ],
    );
    const row = rows([], [g])[0];
    expect(row.label).toBe("ACME");
    expect(row.identifiers.map((i) => i.value)).toEqual(["US0000000001"]);
  });

  it("opens a row other than cash that has a code or a description", () => {
    const gbp = instr("i-gbp", AssetClass.CASH, [
      ident(IdentifierType.CURRENCY, "GBP"),
    ]);
    const coded = instr("i-coded", AssetClass.SECURITY, [
      ident(IdentifierType.ISIN, "US0378331005"),
    ]);
    const bare = instr("i-bare", AssetClass.SECURITY, []);
    const named = group("g-named", [], [described("ibkr", "ACME CORP")]);
    const opens = Object.fromEntries(
      rows([gbp, coded, bare], [named]).map((r) => [r.id, hasDetail(r)]),
    );
    expect(opens).toEqual({
      "i-gbp": false,
      "i-coded": true,
      "i-bare": false,
      "g-named": false,
    });
  });

  it("puts cash first and the rest by label, of either kind", () => {
    const gbp = instr("i-gbp", AssetClass.CASH, [
      ident(IdentifierType.CURRENCY, "GBP"),
    ]);
    const vanguard = instr("i-vusa", AssetClass.SECURITY, [
      ident(IdentifierType.MIC_TICKER, "VUSA"),
    ]);
    const bae = group(
      "g-bae",
      [AssetClass.SECURITY],
      [described("fidelity_uk", "BAE SYSTEMS")],
    );
    expect(rows([vanguard, gbp], [bae]).map((r) => r.id)).toEqual([
      "i-gbp",
      "g-bae",
      "i-vusa",
    ]);
  });

  it("marks a provisional holding and gives each gap its last day", () => {
    const held = create(InstrHoldingSchema, {
      instrId: "i-ge",
      assetClass: AssetClass.STOCK,
      quantity: "10",
      mark: HoldingMark.CORP_ACTS_PROVISIONAL,
      provisionalFrom: "2023-06-02",
      gaps: [
        { uncoveredFrom: "2024-02-01", uncoveredBefore: "2024-03-01" },
        { uncoveredFrom: "2024-04-02", uncoveredBefore: "2024-04-03" },
      ],
    });
    const row = rows([held])[0];
    expect(row.provisional).toBe(true);
    expect(row.provisionalFrom).toBe("2023-06-02");
    expect(row.gaps).toEqual([
      { from: "2024-02-01", to: "2024-02-29" },
      { from: "2024-04-02", to: "2024-04-02" },
    ]);
    expect(hasDetail(row)).toBe(true);
  });

  it("leaves an adjusted holding unmarked", () => {
    const held = create(InstrHoldingSchema, {
      instrId: "i-nvda",
      assetClass: AssetClass.STOCK,
      quantity: "50",
      mark: HoldingMark.ADJUSTED,
    });
    const row = rows([held])[0];
    expect(row.provisional).toBe(false);
    expect(row.gaps).toEqual([]);
    expect(hasDetail(row)).toBe(false);
  });
});
