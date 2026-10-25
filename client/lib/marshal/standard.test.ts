// @vitest-environment node

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  AssetClass,
  Broker,
  IdentifierType,
  StatedKeySchema,
} from "@/gen/type/v1/type_pb";
import {
  RowSchema,
  SplitRatioSchema,
  StatedSplitSchema,
  StatementSchema,
} from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { standard } from "./standard";
import { cash, fixture } from "./test-utils";
import { ident } from "./build";

const text = fixture("standard.json");

const meta = create(StatedKeySchema, {
  identifiers: [ident(IdentifierType.MIC_TICKER, "META", "XNAS")],
  assetClass: AssetClass.STOCK,
  currency: "USD",
});

const expected = create(StatementSchema, {
  broker: Broker.IBKR,
  orderFrom: "2024-01-01",
  orderBefore: "2025-01-01",
  rows: [
    create(RowSchema, {
      key: meta,
      orderDate: "2024-03-01",
      settlementDate: "2024-03-04",
      asAt: "2024-03-01",
      quantity: "10",
    }),
    create(RowSchema, {
      key: cash("USD"),
      orderDate: "2024-03-01",
      settlementDate: "2024-03-04",
      asAt: "2024-03-01",
      quantity: "-4950.25",
    }),
  ],
  splits: [
    create(StatedSplitSchema, {
      key: meta,
      effectiveDate: "2024-06-10",
      quantity: "90",
      ratio: create(SplitRatioSchema, { from: "1", to: "10" }),
    }),
  ],
});

// A document with one row, its fields spelled as the proto names them.
const protoNames = JSON.stringify({
  broker: "BROKER_SCHWAB",
  order_from: "2024-01-01",
  order_before: "2024-02-01",
  rows: [
    {
      key: {
        identifiers: [{ type: "IDENTIFIER_TYPE_CURRENCY", value: "USD" }],
        asset_class: "ASSET_CLASS_CASH",
        currency: "USD",
      },
      order_date: "2024-01-05",
      settlement_date: "2024-01-05",
      as_at: "2024-01-05",
      quantity: "100",
    },
  ],
});

describe("standard", () => {
  it("reads the document as the message it holds", () => {
    expect(standard.marshal(text, "")).toEqual(expected);
  });

  it("accepts the proto spelling of a field", () => {
    const s = standard.marshal(protoNames, "");
    expect(s.broker).toBe(Broker.SCHWAB);
    expect(s.orderFrom).toBe("2024-01-01");
    expect(s.rows[0].asAt).toBe("2024-01-05");
    expect(s.rows[0].key?.assetClass).toBe(AssetClass.CASH);
  });

  it("ignores the export date", () => {
    expect(standard.marshal(text, "2025-01-15")).toEqual(expected);
  });

  it("refuses a document that states no broker or no period", () => {
    expect(() => standard.marshal("{}", "")).toThrow("states no broker");
    expect(() =>
      standard.marshal(JSON.stringify({ broker: "BROKER_IBKR" }), ""),
    ).toThrow("states no period");
  });

  it("refuses a document the parser cannot read, with its message", () => {
    const unknown = () =>
      standard.marshal(JSON.stringify({ broker: "BROKER_IBKR", foo: 1 }), "");
    expect(unknown).toThrow(MarshalError);
    expect(unknown).toThrow('key "foo" is unknown');
    const enumName = () =>
      standard.marshal(JSON.stringify({ broker: "BROKER_FOO" }), "");
    expect(enumName).toThrow(MarshalError);
    expect(enumName).toThrow(/cannot decode .*BROKER_FOO/);
    expect(() => standard.marshal("not json", "")).toThrow(MarshalError);
  });

  it("recognises a JSON document that reads as a statement", () => {
    expect(standard.recognise(text, "application/json")).toBe(true);
    expect(standard.recognise(text, "Application/JSON; charset=utf-8")).toBe(
      true,
    );
    expect(standard.recognise(text, "")).toBe(false);
    expect(standard.recognise(text, "text/plain")).toBe(false);
    expect(standard.recognise(fixture("schwab.json"), "application/json")).toBe(
      false,
    );
    expect(standard.recognise("{}", "application/json")).toBe(false);
    expect(
      standard.recognise(
        JSON.stringify({ broker: "BROKER_IBKR", foo: 1 }),
        "application/json",
      ),
    ).toBe(false);
    expect(standard.recognise(fixture("schwab.csv"), "application/json")).toBe(
      false,
    );
  });
});
