import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { brokerLabel, brokers } from "./broker";

describe("brokerLabel", () => {
  it("names every broker offered", () => {
    expect(brokers.map(brokerLabel)).toEqual(["IBKR", "Schwab", "Fidelity UK"]);
    expect(brokerLabel(Broker.UNSPECIFIED)).toBe("unknown");
  });
});
