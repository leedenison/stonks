import { describe, expect, it } from "vitest";
import { fidelityUkCsv } from "./fidelity-uk-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { formats, marshallerFor, recognisedBy } from "./marshal";
import { schwab } from "./schwab";
import { standard } from "./standard";
import { fixture } from "./test-utils";

describe("marshallerFor", () => {
  it("maps each format to its marshaller", () => {
    expect(marshallerFor("ibkr")).toBe(ibkrQfx);
    expect(marshallerFor("schwab")).toBe(schwab);
    expect(marshallerFor("fidelity_uk")).toBe(fidelityUkCsv);
    expect(marshallerFor("standard")).toBe(standard);
  });

  it("asks an export date of Schwab alone", () => {
    expect(formats.filter((f) => marshallerFor(f).needsExportDate)).toEqual([
      "schwab",
    ]);
  });
});

describe("recognisedBy", () => {
  it("names the one format each file is in", () => {
    expect(
      recognisedBy(fixture("ibkr.qfx"), "application/vnd.intu.qfx"),
    ).toEqual(["ibkr"]);
    expect(recognisedBy(fixture("schwab.json"), "application/json")).toEqual([
      "schwab",
    ]);
    expect(recognisedBy(fixture("schwab.csv"), "text/csv")).toEqual(["schwab"]);
    expect(recognisedBy(fixture("fidelity-uk.csv"), "text/csv")).toEqual([
      "fidelity_uk",
    ]);
    expect(recognisedBy(fixture("standard.json"), "application/json")).toEqual([
      "standard",
    ]);
  });

  it("judges a QFX with no reported type on its contents alone", () => {
    expect(recognisedBy(fixture("ibkr.qfx"), "")).toEqual(["ibkr"]);
  });

  it("needs a type for a CSV or JSON", () => {
    expect(recognisedBy(fixture("fidelity-uk.csv"), "")).toEqual([]);
    expect(recognisedBy(fixture("schwab.json"), "")).toEqual([]);
    expect(recognisedBy(fixture("standard.json"), "")).toEqual([]);
  });

  it("names nobody for text that is no statement", () => {
    expect(recognisedBy("", "text/csv")).toEqual([]);
    expect(recognisedBy("hello,world\n1,2\n", "text/csv")).toEqual([]);
    expect(recognisedBy("{}", "application/json")).toEqual([]);
  });
});
