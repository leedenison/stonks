// @vitest-environment node

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { AssetClass, Broker } from "@/gen/type/v1/type_pb";
import { StatedSplitSchema } from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { cash, fixture, hint, security } from "./test-utils";
import { create as make } from "@bufbuild/protobuf";
import { RowSchema, type Row } from "@/gen/statement/v1/statement_pb";
import type { StatedKey } from "@/gen/type/v1/type_pb";

// row is a leg stated as at the export date, as every Schwab row is.
function row(
  key: StatedKey,
  orderDate: string,
  settlementDate: string,
  quantity: string,
): Row {
  return make(RowSchema, {
    key,
    orderDate,
    settlementDate,
    asAt: EXPORTED,
    quantity,
  });
}
import { schwab } from "./schwab";

const json = fixture("schwab.json");
const csv = fixture("schwab.csv");
const EXPORTED = "2025-01-15";
const statement = schwab.marshal(json, EXPORTED);

const usd = cash("USD");
const stock = (symbol: string, description: string) =>
  security(description, AssetClass.SECURITY, [hint(symbol)], "USD");

describe("schwab", () => {
  it("takes the period from the JSON and states every row as at the export", () => {
    expect(statement.broker).toBe(Broker.SCHWAB);
    expect(statement.rows.every((r) => r.asAt === EXPORTED)).toBe(true);
    expect(statement.orderFrom).toBe("2024-01-01");
    expect(statement.orderBefore).toBe("2025-01-01");
    expect(statement.rows).toHaveLength(17);
  });

  it("derives the period from the rows of the CSV, which carry the same legs", () => {
    const fromCsv = schwab.marshal(csv, EXPORTED);
    expect(fromCsv.orderFrom).toBe("2024-04-17");
    expect(fromCsv.orderBefore).toBe("2024-12-17");
    expect(fromCsv.rows).toEqual(statement.rows);
    expect(fromCsv.splits).toEqual(statement.splits);
  });

  it("emits a dividend, interest and a wire as cash legs", () => {
    expect(statement.rows.slice(0, 3)).toEqual([
      row(usd, "2024-12-16", "2024-12-16", "41.86"),
      row(usd, "2024-11-26", "2024-11-26", "0.42"),
      row(usd, "2024-11-12", "2024-11-12", "-20000"),
    ]);
  });

  it("splits a sell into a negative security leg, gross cash and a fee", () => {
    const googl = stock("GOOGL", "ALPHABET INC CLASS A");
    expect(statement.rows.slice(3, 6)).toEqual([
      row(googl, "2024-11-05", "2024-11-05", "-181"),
      row(usd, "2024-11-05", "2024-11-05", "50462.8"),
      row(usd, "2024-11-05", "2024-11-05", "-0.03"),
    ]);
  });

  it("orders an as-of line on the effective date and settles it on the posted date", () => {
    expect(statement.rows[6]).toEqual(
      row(usd, "2024-10-03", "2024-10-04", "191.71"),
    );
  });

  it("states a split with no ratio", () => {
    expect(statement.splits).toEqual([
      create(StatedSplitSchema, {
        key: stock("TSLA", "TESLA INC"),
        effectiveDate: "2024-08-24",
        quantity: "84",
      }),
    ]);
  });

  it("emits a buy without a fee leg and a merger adjustment as a security leg", () => {
    const googl = stock("GOOGL", "ALPHABET INC CLASS A");
    const atvi = stock(
      "ATVI",
      "ACTIVISION BLIZZARD MANDATORY MERGER EFF: 05/13/24",
    );
    expect(statement.rows.slice(10, 14)).toEqual([
      row(googl, "2024-06-14", "2024-06-14", "22"),
      row(usd, "2024-06-14", "2024-06-14", "-3923.7"),
      row(usd, "2024-05-13", "2024-05-13", "4445.21"),
      row(atvi, "2024-05-13", "2024-05-13", "-46.7917"),
    ]);
  });

  it("fails on an action it does not know, naming the line", () => {
    const odd = csv.replace('"Journal"', '"Journaled Shares"');
    expect(() => schwab.marshal(odd, EXPORTED)).toThrow(MarshalError);
    try {
      schwab.marshal(odd, EXPORTED);
    } catch (e) {
      expect(e).toMatchObject({ line: 10, kind: "Journaled Shares" });
    }
  });
});

describe("schwab.recognise", () => {
  it("recognises both exports by their format", () => {
    expect(schwab.recognise(fixture("schwab.json"), "application/json")).toBe(
      true,
    );
    expect(schwab.recognise(fixture("schwab.csv"), "text/csv")).toBe(true);
    expect(
      schwab.recognise(fixture("schwab.csv"), "application/vnd.ms-excel"),
    ).toBe(true);
  });

  it("refuses a type Schwab does not issue before reading the contents", () => {
    expect(
      schwab.recognise(fixture("schwab.json"), "application/vnd.intu.qfx"),
    ).toBe(false);
  });

  it("needs a reported type, since JSON and CSV parse loosely", () => {
    expect(schwab.recognise(fixture("schwab.json"), "")).toBe(false);
    expect(schwab.recognise(fixture("schwab.csv"), "")).toBe(false);
  });

  it("refuses other text", () => {
    expect(schwab.recognise("", "text/csv")).toBe(false);
    expect(schwab.recognise("{}", "application/json")).toBe(false);
    expect(
      schwab.recognise('{"BrokerageTransactions": []}', "application/json"),
    ).toBe(false);
    expect(schwab.recognise('"Date","Action"\n', "text/csv")).toBe(false);
    expect(schwab.recognise(fixture("fidelity-uk.csv"), "text/csv")).toBe(
      false,
    );
    expect(schwab.recognise(fixture("ibkr.qfx"), "application/x-ofx")).toBe(
      false,
    );
  });
});
