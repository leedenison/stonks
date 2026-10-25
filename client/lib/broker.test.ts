import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { formats } from "@/lib/marshal/marshal";
import { brokerLabel, formatLabel } from "./broker";

describe("labels", () => {
  it("name every broker and every format offered", () => {
    expect(brokerLabel(Broker.IBKR)).toBe("IBKR");
    expect(brokerLabel(Broker.UNSPECIFIED)).toBe("unknown");
    expect(formats.map(formatLabel)).toEqual([
      "IBKR export",
      "Schwab export",
      "Fidelity UK export",
      "Standard format",
    ]);
  });
});
