import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { IdentifierSchema, IdentifierType } from "@/gen/type/v1/type_pb";
import { groupIdentifiers, identifierKey, pick } from "./identifiers";

function ident(type: IdentifierType, value: string, domain = "") {
  return create(IdentifierSchema, { type, value, domain });
}

describe("pick", () => {
  it("walks the order given", () => {
    const ids = [
      ident(IdentifierType.BROKER_ID, "1", "ibkr"),
      ident(IdentifierType.ISIN, "US0378331005"),
    ];
    expect(
      pick([IdentifierType.MIC_TICKER, IdentifierType.ISIN], ids)?.type,
    ).toBe(IdentifierType.ISIN);
    expect(pick([IdentifierType.BROKER_ID], ids)?.value).toBe("1");
    expect(pick([IdentifierType.SEDOL], ids)).toBeUndefined();
  });
});

describe("groupIdentifiers", () => {
  it("gathers the domains of one value and keeps the types in the order given, via groups first within a type", () => {
    const ids = [
      ident(IdentifierType.OPENFIGI_COMPOSITE, "BBG000B9XRY4"),
      ident(IdentifierType.MIC_TICKER, "ACME", "XNYS"),
      ident(IdentifierType.MIC_TICKER, "ACM", "XLON"),
      ident(IdentifierType.MIC_TICKER, "ACME", "XNAS"),
      ident(IdentifierType.SEDOL, "0000000"),
      ident(IdentifierType.CURRENCY, "USD"),
    ];
    const marks = {
      via: new Set([
        identifierKey(ident(IdentifierType.MIC_TICKER, "ACME", "XNAS")),
      ]),
      name: new Set([
        identifierKey(ident(IdentifierType.MIC_TICKER, "ACME", "XNYS")),
      ]),
    };
    const plain = { via: false, name: false };
    expect(groupIdentifiers(ids, marks)).toEqual([
      {
        type: IdentifierType.OPENFIGI_COMPOSITE,
        value: "BBG000B9XRY4",
        domains: [],
        ...plain,
      },
      {
        type: IdentifierType.MIC_TICKER,
        value: "ACME",
        domains: ["XNYS", "XNAS"],
        via: true,
        name: true,
      },
      {
        type: IdentifierType.MIC_TICKER,
        value: "ACM",
        domains: ["XLON"],
        ...plain,
      },
      { type: IdentifierType.SEDOL, value: "0000000", domains: [], ...plain },
      { type: IdentifierType.CURRENCY, value: "USD", domains: [], ...plain },
    ]);
    expect(groupIdentifiers([])).toEqual([]);
  });
});
