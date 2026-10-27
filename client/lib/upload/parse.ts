import type { Stmt } from "@/gen/stmt/v1/stmt_pb";
import {
  type Format,
  MarshalError,
  marshallerFor,
} from "@/lib/marshal/marshal";

export type Parsed =
  { stmt: Stmt; error?: undefined } | { stmt?: undefined; error: MarshalError };

// parseExport marshals text in the given format. A failure is returned
// rather than thrown, so a stage of the dialog can show it and offer
// another file. An error from inside a parser, on a file of another kind
// entirely, is wrapped so the caller sees one shape.
export function parseExport(
  text: string,
  format: Format,
  exportedOn: string,
): Parsed {
  try {
    return { stmt: marshallerFor(format).marshal(text, exportedOn) };
  } catch (e) {
    if (e instanceof MarshalError) {
      return { error: e };
    }
    return {
      error: new MarshalError(e instanceof Error ? e.message : String(e)),
    };
  }
}
