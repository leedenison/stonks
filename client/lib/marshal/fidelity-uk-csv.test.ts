// @vitest-environment node

import { describe, expect, it } from "vitest";
import { AssetClass, Broker } from "@/gen/type/v1/type_pb";
import { MarshalError } from "./error";
import { fidelityUkCsv } from "./fidelity-uk-csv";
import { cash, fixture, hint, row, security } from "./test-utils";

const text = fixture("fidelity-uk.csv");
const statement = fidelityUkCsv.marshal(text);

const gbp = cash("GBP");

describe("fidelityUkCsv", () => {
  it("takes the period from the preamble, cut at the earliest pending line", () => {
    expect(statement.broker).toBe(Broker.FIDELITY_UK);
    expect(statement.orderFrom).toBe("2025-01-01");
    expect(statement.orderBefore).toBe("2025-03-17");
    expect(statement.rows).toHaveLength(11);
    expect(statement.splits).toEqual([]);
  });

  // The e2e suite narrows the period to February and expects these rejected.
  it("states four rows before February", () => {
    expect(
      statement.rows.filter((r) => r.orderDate < "2025-02-01"),
    ).toHaveLength(4);
  });

  it("claims the whole timeframe when nothing is pending", () => {
    const settled = text.replace(
      "17 Mar 2025,Pending",
      "17 Mar 2025,21 Mar 2025",
    );
    const all = fidelityUkCsv.marshal(settled);
    expect(all.orderBefore).toBe("2025-04-01");
    expect(all.rows).toHaveLength(14);
    expect(all.rows[2]).toEqual(row(gbp, "2025-03-17", "2025-03-21", "-0.03"));
  });

  it("ignores a zero charge awaiting completion", () => {
    const zero = text.replace(
      'Pending,Tax On Interest,"Cash",Cash Management Account,AW10000003,,-0.03,0,0',
      'Pending,Dealing Fee,"Cash",Cash Management Account,AW10000003,,0.00,0,0',
    );
    const all = fidelityUkCsv.marshal(zero);
    expect(all.orderBefore).toBe("2025-04-01");
    expect(all.rows).toHaveLength(13);
  });

  it("claims nothing when the earliest pending line opens the timeframe", () => {
    const first = text.replace(
      "02 Jan 2025,01 Jan 2025",
      "01 Jan 2025,Pending",
    );
    const none = fidelityUkCsv.marshal(first);
    expect(none.rows).toEqual([]);
    expect(none.orderFrom).toBe("");
    expect(none.orderBefore).toBe("");
  });

  it("emits a cash line as a cash leg of its amount, whatever its type", () => {
    expect(statement.rows[0]).toEqual(
      row(gbp, "2025-03-06", "2025-03-09", "-5.4"),
    );
  });

  it("negates a sell and leaves its cash and fee lines as their own legs", () => {
    const vusa = security(
      "VANGUARD FUNDS PLC, S&P 500 UCITS ETF USD DIS (VUSA)",
      AssetClass.SECURITY,
      [hint("VUSA")],
      "GBP",
    );
    expect(statement.rows.slice(1, 4)).toEqual([
      row(vusa, "2025-02-24", "2025-02-26", "-141"),
      row(gbp, "2025-02-24", "2025-02-26", "13587.84"),
      row(gbp, "2025-02-24", "2025-02-24", "-7.5"),
    ]);
  });

  it("carries a symbol with a dot and none where the name states none", () => {
    const bae = security(
      "BAE SYSTEMS, ORD GBP0.025 (BA.)",
      AssetClass.SECURITY,
      [hint("BA.")],
      "GBP",
    );
    const fund = security(
      "Baillie Gifford Responsible Global Equity Income B Inc",
      AssetClass.SECURITY,
      [],
      "GBP",
    );
    expect(statement.rows[4]).toEqual(
      row(bae, "2025-02-10", "2025-02-12", "120"),
    );
    expect(statement.rows[7]).toEqual(
      row(fund, "2025-01-22", "2025-02-06", "19.26"),
    );
  });

  it("keeps a settlement before its order date", () => {
    expect(statement.rows[10]).toEqual(
      row(gbp, "2025-01-02", "2025-01-01", "25.35"),
    );
  });

  it("does not emit a cancelled line or one awaiting completion", () => {
    expect(
      statement.rows.filter((r) => r.orderDate === "2025-02-10"),
    ).toHaveLength(3);
    expect(statement.rows.filter((r) => r.quantity === "-0.03")).toHaveLength(
      0,
    );
  });

  it("reads a header without the trailing comma alike", () => {
    const trimmed = text.replace(/,(\r\n|$)/g, "$1");
    expect(fidelityUkCsv.marshal(trimmed)).toEqual(statement);
  });

  it("fails on a security line of a type it does not know, naming the line", () => {
    const odd = text.replace("Reinvestment From Income", "Switch In");
    expect(() => fidelityUkCsv.marshal(odd)).toThrow(MarshalError);
    try {
      fidelityUkCsv.marshal(odd);
    } catch (e) {
      expect(e).toMatchObject({ line: 19, kind: "Switch In" });
    }
  });
});

describe("fidelityUkCsv.recognise", () => {
  const CSV = "text/csv";

  it("recognises an export whose lines state Fidelity UK account numbers", () => {
    expect(fidelityUkCsv.recognise(text, CSV)).toBe(true);
    expect(fidelityUkCsv.recognise(text, "application/vnd.ms-excel")).toBe(
      true,
    );
  });

  it("refuses a type Fidelity UK does not issue before reading the contents", () => {
    expect(fidelityUkCsv.recognise(text, "application/json")).toBe(false);
  });

  it("needs a reported type, since CSV parses loosely", () => {
    expect(fidelityUkCsv.recognise(text, "")).toBe(false);
  });

  it("refuses an export whose account numbers take another form", () => {
    expect(
      fidelityUkCsv.recognise(
        text.replace(/,A[SGW]1000000(\d),/g, ",1000000$1,"),
        CSV,
      ),
    ).toBe(false);
    expect(
      fidelityUkCsv.recognise(text.replace(",AS10000001,", ",AS1000001,"), CSV),
    ).toBe(false);
  });

  it("refuses an export without the timeframe or the column", () => {
    expect(
      fidelityUkCsv.recognise(text.replace("Timeframe,", "Period,"), CSV),
    ).toBe(false);
    expect(
      fidelityUkCsv.recognise(text.replace("Account Number", "Account"), CSV),
    ).toBe(false);
    expect(fidelityUkCsv.recognise("", CSV)).toBe(false);
    expect(fidelityUkCsv.recognise(fixture("schwab.csv"), CSV)).toBe(false);
  });
});
