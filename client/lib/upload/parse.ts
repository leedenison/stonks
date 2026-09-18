import type { Statement } from "@/gen/statement/v1/statement_pb";
import { Broker } from "@/gen/type/v1/type_pb";
import { MarshalError, marshallerFor } from "@/lib/marshal/marshal";

export type Parsed =
  | { statement: Statement; error?: undefined }
  | { statement?: undefined; error: MarshalError };

// parseExport marshals text as an export of broker. A failure is returned
// rather than thrown, so a stage of the dialog can show it and offer
// another file. An error from inside a parser, on a file of another kind
// entirely, is wrapped so the caller sees one shape.
export function parseExport(
  text: string,
  broker: Broker,
  exportedOn: string,
): Parsed {
  try {
    return { statement: marshallerFor(broker).marshal(text, exportedOn) };
  } catch (e) {
    if (e instanceof MarshalError) {
      return { error: e };
    }
    return {
      error: new MarshalError(e instanceof Error ? e.message : String(e)),
    };
  }
}

// needsExportDate is true for a broker whose export restates every quantity
// to the units held when it was taken, so the date it was taken is part of
// the statement.
export function needsExportDate(broker: Broker): boolean {
  return broker === Broker.SCHWAB;
}
