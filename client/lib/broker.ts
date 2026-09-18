import { Broker } from "@/gen/type/v1/type_pb";

// brokerLabel names a broker as it is shown to the user.
export function brokerLabel(broker: Broker): string {
  switch (broker) {
    case Broker.IBKR:
      return "IBKR";
    case Broker.SCHWAB:
      return "Schwab";
    case Broker.FIDELITY_UK:
      return "Fidelity UK";
    default:
      return "unknown";
  }
}

// The brokers a user can upload a statement from, in the order offered.
export const brokers = [Broker.IBKR, Broker.SCHWAB, Broker.FIDELITY_UK];
