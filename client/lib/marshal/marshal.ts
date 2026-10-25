// A marshaller reads a file in one format into the Statement message in
// proto/statement/v1/statement.proto. A format is a broker's export or the
// standard format. A broker marshaller holds all knowledge of its broker's
// conventions. A line of a kind it does not know fails the whole export.

import type { Statement } from "@/gen/statement/v1/statement_pb";
import { MarshalError } from "./error";
import { fidelityUkCsv } from "./fidelity-uk-csv";
import { ibkrQfx } from "./ibkr-qfx";
import { schwab } from "./schwab";
import { standard } from "./standard";

export { MarshalError };

export interface Marshaller {
  // exportedOn is the date the export was taken. It is ignored unless
  // needsExportDate is true.
  marshal(text: string, exportedOn: string): Statement;
  // type is the media type the browser reports for the file. recognise never
  // throws.
  recognise(text: string, type: string): boolean;
  // needsExportDate is true for a format that restates every quantity to the
  // units held when the export was taken.
  needsExportDate: boolean;
}

// Format names one kind of file the upload dialog takes. A broker export
// fixes its broker. The standard format states its own.
export type Format = "ibkr" | "schwab" | "fidelity_uk" | "standard";

const marshallers: Record<Format, Marshaller> = {
  ibkr: ibkrQfx,
  schwab,
  fidelity_uk: fidelityUkCsv,
  standard,
};

// formats lists every format in the order the dialog offers them.
export const formats = Object.keys(marshallers) as Format[];

export function marshallerFor(format: Format): Marshaller {
  return marshallers[format];
}

// recognisedBy returns the formats whose marshaller recognises text.
export function recognisedBy(text: string, type: string): Format[] {
  return formats.filter((f) => marshallers[f].recognise(text, type));
}
