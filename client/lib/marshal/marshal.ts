// A marshaller translates one broker's export into the broker neutral
// format in proto/statement/v1/statement.proto. All knowledge of the
// broker's conventions live in the marshaller. A line of a kind the
// marshaller does not know fails the whole export.

import { Broker } from "@/gen/type/v1/type_pb";
import type { Statement } from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { fidelityUkCsv } from "./fidelity-uk-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { schwab } from "./schwab";

export { MarshalError };

export interface Marshaller {
  // exportedOn is the date the export was taken, for a broker that restates
  // every quantity to the units held at export.
  marshal(text: string, exportedOn: string): Statement;
  // type is the media type the browser reports for the file. recognise never
  // throws.
  recognise(text: string, type: string): boolean;
}

const marshallers: [Broker, Marshaller][] = [
  [Broker.IBKR, ibkrQfx],
  [Broker.SCHWAB, schwab],
  [Broker.FIDELITY_UK, fidelityUkCsv],
];

export function marshallerFor(broker: Broker): Marshaller {
  const found = marshallers.find(([b]) => b === broker);
  if (!found) {
    throw new MarshalError(`no marshaller for broker ${broker}`);
  }
  return found[1];
}

// recognisedBy returns the brokers whose marshaller recognises text.
export function recognisedBy(text: string, type: string): Broker[] {
  return marshallers.filter(([, m]) => m.recognise(text, type)).map(([b]) => b);
}
