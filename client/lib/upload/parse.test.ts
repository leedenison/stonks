import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { MarshalError } from "@/lib/marshal/marshal";
import { fixture } from "../marshal/test-utils";
import { parseExport } from "./parse";

describe("parseExport", () => {
  it("marshals a file of the right format", () => {
    const parsed = parseExport(fixture("fidelity-uk.csv"), "fidelity_uk");
    expect(parsed.error).toBeUndefined();
    expect(parsed.stmt?.broker).toBe(Broker.FIDELITY_UK);
    expect(parsed.stmt?.rows.length).toBeGreaterThan(0);
  });

  it("returns rather than throws on the wrong format", () => {
    const parsed = parseExport(fixture("fidelity-uk.csv"), "schwab");
    expect(parsed.stmt).toBeUndefined();
    expect(parsed.error).toBeInstanceOf(MarshalError);
    expect(parseExport("", "ibkr").error).toBeInstanceOf(MarshalError);
  });
});
