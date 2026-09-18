import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { fidelityUkCsv } from "./fidelity-uk-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { MarshalError, marshallerFor, recognisedBy } from "./marshal";
import { schwab } from "./schwab";
import { fixture } from "./test-utils";

describe("marshallerFor", () => {
  it("maps each broker to its marshaller", () => {
    expect(marshallerFor(Broker.IBKR)).toBe(ibkrQfx);
    expect(marshallerFor(Broker.SCHWAB)).toBe(schwab);
    expect(marshallerFor(Broker.FIDELITY_UK)).toBe(fidelityUkCsv);
  });

  it("refuses an unspecified broker", () => {
    expect(() => marshallerFor(Broker.UNSPECIFIED)).toThrow(MarshalError);
  });
});

describe("recognisedBy", () => {
  it("names the one broker each export is from", () => {
    expect(
      recognisedBy(fixture("ibkr.qfx"), "application/vnd.intu.qfx"),
    ).toEqual([Broker.IBKR]);
    expect(recognisedBy(fixture("schwab.json"), "application/json")).toEqual([
      Broker.SCHWAB,
    ]);
    expect(recognisedBy(fixture("schwab.csv"), "text/csv")).toEqual([
      Broker.SCHWAB,
    ]);
    expect(recognisedBy(fixture("fidelity-uk.csv"), "text/csv")).toEqual([
      Broker.FIDELITY_UK,
    ]);
  });

  it("names nobody for a file with no reported type", () => {
    expect(recognisedBy(fixture("ibkr.qfx"), "")).toEqual([]);
    expect(recognisedBy(fixture("fidelity-uk.csv"), "")).toEqual([]);
  });

  it("names nobody for text that is no export", () => {
    expect(recognisedBy("", "text/csv")).toEqual([]);
    expect(recognisedBy("hello,world\n1,2\n", "text/csv")).toEqual([]);
    expect(recognisedBy("{}", "application/json")).toEqual([]);
  });
});
