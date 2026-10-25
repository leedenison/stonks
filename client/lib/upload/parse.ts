import type { Statement } from "@/gen/statement/v1/statement_pb";
import {
  type Format,
  MarshalError,
  marshallerFor,
} from "@/lib/marshal/marshal";

export type Parsed =
  | { statement: Statement; error?: undefined }
  | { statement?: undefined; error: MarshalError };

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
    return { statement: marshallerFor(format).marshal(text, exportedOn) };
  } catch (e) {
    if (e instanceof MarshalError) {
      return { error: e };
    }
    return {
      error: new MarshalError(e instanceof Error ? e.message : String(e)),
    };
  }
}
