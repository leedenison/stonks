import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { fidelityCsv } from "./fidelity-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { MarshalError, marshallerFor } from "./marshal";
import { schwab } from "./schwab";

describe("marshallerFor", () => {
  it("maps each broker to its marshaller", () => {
    expect(marshallerFor(Broker.IBKR)).toBe(ibkrQfx);
    expect(marshallerFor(Broker.SCHWAB)).toBe(schwab);
    expect(marshallerFor(Broker.FIDELITY)).toBe(fidelityCsv);
  });

  it("refuses an unspecified broker", () => {
    expect(() => marshallerFor(Broker.UNSPECIFIED)).toThrow(MarshalError);
  });
});
