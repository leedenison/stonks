// A marshaller translates one broker's export, given as its text, into the
// neutral format in proto/statement/v1/statement.proto. All knowledge of the
// broker's conventions lives here: which lines are legs, which dates are
// stated, which identifiers the export carries, and which lines are not
// emitted. A line of a kind the marshaller does not know fails the whole
// export, so no leg is silently dropped.

import { Broker } from "@/gen/type/v1/type_pb";
import type { Statement } from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { fidelityCsv } from "./fidelity-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { schwab } from "./schwab";

export { MarshalError };

export interface Marshaller {
  // exportedOn is the date the export was taken, for a broker that restates
  // every quantity to the units held at export.
  marshal(text: string, exportedOn: string): Statement;
}

export function marshallerFor(broker: Broker): Marshaller {
  switch (broker) {
    case Broker.IBKR:
      return ibkrQfx;
    case Broker.SCHWAB:
      return schwab;
    case Broker.FIDELITY:
      return fidelityCsv;
    default:
      throw new MarshalError(`no marshaller for broker ${broker}`);
  }
}
