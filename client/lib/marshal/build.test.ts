import { describe, expect, it } from "vitest";
import { Broker } from "@/gen/type/v1/type_pb";
import { brokerDomain } from "./build";

describe("brokerDomain", () => {
  it("names each broker as the server's broker vocabulary does", () => {
    expect(brokerDomain(Broker.IBKR)).toBe("ibkr");
    expect(brokerDomain(Broker.SCHWAB)).toBe("schwab");
    expect(brokerDomain(Broker.FIDELITY_UK)).toBe("fidelity_uk");
  });
});
