// @vitest-environment node

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  AssetClass,
  Broker,
  IdentifierSchema,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import {
  SplitRatioSchema,
  StatedSplitSchema,
} from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { cash, fixture, hint, row, security } from "./test-utils";
import { ibkrQfx } from "./ibkr-qfx";

// The BUYSTOCK carries an invented nonzero TAXES; every real statement seen
// states 0.
const text = fixture("ibkr.qfx");
const statement = ibkrQfx.marshal(text);

const cusip = (value: string) =>
  create(IdentifierSchema, { type: IdentifierType.CUSIP, value });
const isin = (value: string) =>
  create(IdentifierSchema, { type: IdentifierType.ISIN, value });
const option = (value: string) =>
  create(IdentifierSchema, { type: IdentifierType.OPTION, value });
const conid = (value: string) =>
  create(IdentifierSchema, {
    type: IdentifierType.BROKER_ID,
    value,
    domain: "ibkr",
  });

describe("ibkrQfx", () => {
  it("claims the stated period, not one widened to the rows", () => {
    expect(statement.broker).toBe(Broker.IBKR);
    expect(statement.orderFrom).toBe("2024-01-01");
    expect(statement.orderBefore).toBe("2024-03-30");
    expect(statement.rows).toHaveLength(20);
    expect(statement.splits).toHaveLength(2);
  });

  it("splits a buy into security, gross cash, fee and tax legs that sum to the total", () => {
    const amd = security(
      "AMD ADVANCED MICRO DEVICES",
      AssetClass.EQUITY,
      [cusip("007903107"), hint("AMD")],
      "USD",
    );
    expect(statement.rows.slice(0, 4)).toEqual([
      row(amd, "2024-01-15", "2024-01-15", "200"),
      row(cash("USD"), "2024-01-15", "2024-01-15", "-14846.5602968"),
      row(cash("USD"), "2024-01-15", "2024-01-15", "-0.78710536"),
      row(cash("USD"), "2024-01-15", "2024-01-15", "-1.5"),
    ]);
  });

  it("emits a sell with no tax leg, the ticker stripped of its venue suffix", () => {
    const rhm = security(
      "RHMd RHEINMETALL AG",
      AssetClass.EQUITY,
      [isin("DE0007030009"), hint("RHM")],
      "EUR",
    );
    expect(statement.rows.slice(4, 7)).toEqual([
      row(rhm, "2024-01-23", "2024-01-23", "-10"),
      row(cash("EUR"), "2024-01-23", "2024-01-23", "6094.72188"),
      row(cash("EUR"), "2024-01-23", "2024-01-23", "-3.04736094"),
    ]);
  });

  it("names an option by its contract id and the symbol its terms build", () => {
    const put = security(
      "NVDA  240315P00420000 NVDA 15MAR24 420 P",
      AssetClass.OPTION,
      [conid("624291205"), option("NVDA  240315P00420000")],
      "USD",
    );
    expect(statement.rows[7]).toEqual(
      row(put, "2024-02-20", "2024-02-20", "1"),
    );
    expect(statement.rows[10]).toEqual(
      row(put, "2024-03-21", "2024-03-21", "-1"),
    );
    expect(statement.rows[11].quantity).toBe("1222.64415");
  });

  it("keeps the stated date of an evening posting", () => {
    expect(statement.rows[13]).toEqual(
      row(cash("USD"), "2024-01-11", "2024-01-11", "96.9378592"),
    );
  });

  it("emits every bank transaction as a cash leg, including one before the period", () => {
    expect(statement.rows.slice(14, 19)).toEqual([
      row(cash("USD"), "2024-02-05", "2024-02-05", "12.34"),
      row(cash("GBP"), "2024-01-10", "2024-01-10", "50000"),
      row(cash("USD"), "2023-12-15", "2023-12-15", "-14.54"),
      row(cash("USD"), "2024-01-03", "2024-01-03", "-22880.11960797"),
      row(cash("GBP"), "2024-01-03", "2024-01-03", "18000"),
    ]);
  });

  it("emits a transfer that is not a split as a security leg with no currency", () => {
    const djt = security(
      "DJT TRUMP MEDIA & TECHNOLOGY GRO",
      AssetClass.EQUITY,
      [cusip("25400Q105"), hint("DJT")],
    );
    expect(statement.rows[19]).toEqual(
      row(djt, "2024-03-01", "2024-03-01", "100"),
    );
  });

  it("states a split from a transfer, with the ratio from the memo", () => {
    expect(statement.splits).toEqual([
      create(StatedSplitSchema, {
        key: security("AMZN AMAZON.COM INC", AssetClass.EQUITY, [
          cusip("023135106"),
          hint("AMZN"),
        ]),
        effectiveDate: "2024-03-15",
        quantity: "1007",
        ratio: create(SplitRatioSchema, { from: "1", to: "20" }),
      }),
      create(StatedSplitSchema, {
        key: security(
          "NVDA  241115P00091000 NVDA 15NOV24 91 P",
          AssetClass.OPTION,
          [conid("678941159"), option("NVDA  241115P00091000")],
        ),
        effectiveDate: "2024-03-15",
        quantity: "-18",
        ratio: create(SplitRatioSchema, { from: "1", to: "10" }),
      }),
    ]);
  });

  it("reads a list with one element the same as one with several", () => {
    const one = text
      .replace(/<SELLSTOCK>[\s\S]*<\/SELLSTOCK>/, "")
      .replace(/<BUYOPT>[\s\S]*<\/SELLOPT>/, "")
      .replace(/<INCOME>[\s\S]*<\/INVBANKTRAN>/, "")
      .replace(/<TRANSFER>[\s\S]*<\/TRANSFER>/, "")
      .replace(/<OPTINFO>[\s\S]*<\/OPTINFO>/, "");
    expect(ibkrQfx.marshal(one).rows).toEqual(statement.rows.slice(0, 4));
  });

  it("takes the account's currency for a trade that states none, key and cash alike", () => {
    const bare = text.replace(/<CURRENCY>[\s\S]*?<\/CURRENCY>/, "");
    const rows = ibkrQfx.marshal(bare).rows;
    expect(rows[0].key?.currency).toBe("GBP");
    expect(rows.slice(1, 4).map((r) => r.key)).toEqual([
      cash("GBP"),
      cash("GBP"),
      cash("GBP"),
    ]);
  });

  it("writes a share class separator as a dot", () => {
    const berk = text.replace("<TICKER>AMD</TICKER>", "<TICKER>BRK B</TICKER>");
    expect(ibkrQfx.marshal(berk).rows[0].key?.identifiers).toContainEqual(
      hint("BRK.B"),
    );
  });

  it("states the ticker of a fund and omits the ticker of a debt security", () => {
    const amd =
      /<STOCKINFO>(\s*<SECINFO>\s*<SECID>\s*<UNIQUEID>007903107[\s\S]*?<\/SECINFO>\s*)<\/STOCKINFO>/;
    expect(text).toMatch(amd);
    const fund = ibkrQfx.marshal(text.replace(amd, "<MFINFO>$1</MFINFO>"));
    expect(fund.rows[0].key?.assetClass).toBe(AssetClass.MUTUAL_FUND);
    expect(fund.rows[0].key?.identifiers).toContainEqual(hint("AMD"));
    const debt = ibkrQfx.marshal(text.replace(amd, "<DEBTINFO>$1</DEBTINFO>"));
    expect(debt.rows[0].key?.assetClass).toBe(AssetClass.FIXED_INCOME);
    expect(debt.rows[0].key?.identifiers.map((i) => i.type)).not.toContain(
      IdentifierType.MIC_TICKER,
    );
  });

  it("builds the symbol from the terms for a ticker in the broker's own form", () => {
    const own = text
      .replace(
        "<SECNAME>NVDA  240315P00420000 NVDA 15MAR24 420 P</SECNAME>",
        "<SECNAME>P NVDA  20240315 420 M NVDA 15MAR24 420 P</SECNAME>",
      )
      .replace(
        "<TICKER>NVDA  240315P00420000</TICKER>",
        "<TICKER>P NVDA  20240315 420 M</TICKER>",
      );
    const ids = ibkrQfx.marshal(own).rows[7].key?.identifiers ?? [];
    expect(ids).toEqual([
      conid("624291205"),
      option("NVDA  240315P00420000"),
      expect.objectContaining({ type: IdentifierType.BROKER_DESCRIPTION }),
    ]);
  });

  it("reads the root from the name", () => {
    const eurex = text
      .replace(
        "<SECNAME>NVDA  240315P00420000 NVDA 15MAR24 420 P</SECNAME>",
        "<SECNAME>P RHM  20240315 420 M RHM 15MAR24 420 P</SECNAME>",
      )
      .replace(
        "<TICKER>NVDA  240315P00420000</TICKER>",
        "<TICKER>P RHM  20240315 420 M</TICKER>",
      );
    expect(ibkrQfx.marshal(eurex).rows[7].key?.identifiers).toContainEqual(
      option("RHM   240315P00420000"),
    );
  });

  it("joins a share class onto the root as OCC spells it", () => {
    const berk = text
      .replace(
        "<SECNAME>NVDA  240315P00420000 NVDA 15MAR24 420 P</SECNAME>",
        "<SECNAME>BRKB  240315P00420000 BRK B 15MAR24 420 P</SECNAME>",
      )
      .replace(
        "<TICKER>NVDA  240315P00420000</TICKER>",
        "<TICKER>BRKB  240315P00420000</TICKER>",
      );
    expect(ibkrQfx.marshal(berk).rows[7].key?.identifiers).toContainEqual(
      option("BRKB  240315P00420000"),
    );
  });

  it("fails on an option name that does not start with its ticker", () => {
    const odd = text.replace(
      "<SECNAME>NVDA  240315P00420000 NVDA 15MAR24 420 P</SECNAME>",
      "<SECNAME>NVDA 15MAR24 420 P</SECNAME>",
    );
    expect(() => ibkrQfx.marshal(odd)).toThrow("malformed option");
  });

  it("fails when an OCC ticker and the option's terms disagree", () => {
    const odd = text.replace(
      "<STRIKEPRICE>420</STRIKEPRICE>",
      "<STRIKEPRICE>425</STRIKEPRICE>",
    );
    expect(() => ibkrQfx.marshal(odd)).toThrow(
      "printed as NVDA  240315P00420000 but its terms name NVDA  240315P00425000",
    );
  });

  it("fails on an element it does not know", () => {
    const odd = text
      .replace(/<INCOME>/, "<RETOFCAP>")
      .replace(/<\/INCOME>/, "</RETOFCAP>");
    expect(() => ibkrQfx.marshal(odd)).toThrow(MarshalError);
    expect(() => ibkrQfx.marshal(odd)).toThrow("unknown element RETOFCAP");
  });

  it("fails on a security the list does not name", () => {
    const odd = text.replace(
      "<UNIQUEID>007903107</UNIQUEID>",
      "<UNIQUEID>007903199</UNIQUEID>",
    );
    expect(() => ibkrQfx.marshal(odd)).toThrow("not in SECLIST");
  });
});

describe("ibkrQfx.recognise", () => {
  const qfx = fixture("ibkr.qfx");
  const QFX = "application/vnd.intu.qfx";

  it("recognises a statement from an IBKR account", () => {
    expect(ibkrQfx.recognise(qfx, QFX)).toBe(true);
    expect(ibkrQfx.recognise(qfx, "Application/X-OFX; charset=x")).toBe(true);
  });

  it("refuses a type IBKR does not issue before reading the contents", () => {
    expect(ibkrQfx.recognise(qfx, "text/csv")).toBe(false);
  });

  it("takes a QFX with no reported type on its contents alone", () => {
    expect(ibkrQfx.recognise(qfx, "")).toBe(true);
  });

  it("refuses a statement from an account of another form", () => {
    expect(
      ibkrQfx.recognise(
        qfx.replace("<ACCTID>U1234567", "<ACCTID>12345678"),
        QFX,
      ),
    ).toBe(false);
    expect(
      ibkrQfx.recognise(
        qfx.replace("<ACCTID>U1234567", "<ACCTID>1234567U"),
        QFX,
      ),
    ).toBe(false);
  });

  it("refuses text that is no OFX statement", () => {
    expect(ibkrQfx.recognise("", QFX)).toBe(false);
    expect(ibkrQfx.recognise("OFXHEADER:100\nDATA:OFXSGML\n", QFX)).toBe(false);
    expect(ibkrQfx.recognise(fixture("schwab.csv"), QFX)).toBe(false);
  });
});
