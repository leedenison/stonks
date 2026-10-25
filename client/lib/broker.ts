// Labels for brokers and formats as they are shown to the user.

import { Broker } from "@/gen/type/v1/type_pb";
import type { Format } from "@/lib/marshal/marshal";

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

export function formatLabel(format: Format): string {
  switch (format) {
    case "ibkr":
      return "IBKR export";
    case "schwab":
      return "Schwab export";
    case "fidelity_uk":
      return "Fidelity UK export";
    case "standard":
      return "Standard format";
  }
}
