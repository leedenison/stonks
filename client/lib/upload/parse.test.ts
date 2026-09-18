import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { MarshalError } from "@/lib/marshal/marshal";
import { fixture } from "../marshal/test-utils";
import { needsExportDate, parseExport } from "./parse";

describe("parseExport", () => {
  it("marshals an export of the right broker", () => {
    const parsed = parseExport(
      fixture("fidelity-uk.csv"),
      Broker.FIDELITY_UK,
      "",
    );
    expect(parsed.error).toBeUndefined();
    expect(parsed.statement?.broker).toBe(Broker.FIDELITY_UK);
    expect(parsed.statement?.rows.length).toBeGreaterThan(0);
  });

  it("takes the export date for a broker that needs one", () => {
    const parsed = parseExport(
      fixture("schwab.csv"),
      Broker.SCHWAB,
      "2025-01-15",
    );
    expect(parsed.statement?.rows[0].asAt).toBe("2025-01-15");
  });

  it("returns rather than throws on the wrong broker", () => {
    const parsed = parseExport(fixture("fidelity-uk.csv"), Broker.SCHWAB, "");
    expect(parsed.statement).toBeUndefined();
    expect(parsed.error).toBeInstanceOf(MarshalError);
    expect(parseExport("", Broker.IBKR, "").error).toBeInstanceOf(MarshalError);
  });

  it("knows which broker needs an export date", () => {
    expect(needsExportDate(Broker.SCHWAB)).toBe(true);
    expect(needsExportDate(Broker.IBKR)).toBe(false);
    expect(needsExportDate(Broker.FIDELITY_UK)).toBe(false);
  });
});
