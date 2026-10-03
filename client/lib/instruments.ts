import type {
  Instrument,
  Listing,
  ListInstrumentsResponse,
} from "@/gen/instrument/v1/instrument_pb";
import { AssetClass, type Identifier } from "@/gen/type/v1/type_pb";
import { nameOf } from "./identifiers";

// An InstrumentRow is one line of the instruments table.
export type InstrumentRow = {
  id: string;
  label: string;
  assetClass: AssetClass;
  // The instrument grain identifiers shown beside the label, excluding the
  // one that names the instrument.
  identifiers: Identifier[];
  listings: Listing[];
};

// instrumentRows is the table's rows, cash first and the rest by label. An
// instrument is named by the rule the holdings page uses, over its own
// identifiers and its listings' together.
export function instrumentRows(
  res: ListInstrumentsResponse | undefined,
): InstrumentRow[] {
  const rows = (res?.instruments ?? []).map((i: Instrument): InstrumentRow => {
    const all = [...i.identifiers, ...i.listings.flatMap((l) => l.identifiers)];
    const name = nameOf(i.assetClass, all);
    return {
      id: i.id,
      label: name?.value ?? i.id,
      assetClass: i.assetClass,
      identifiers: i.identifiers.filter((x) => x !== name),
      listings: i.listings,
    };
  });
  const rank = (r: InstrumentRow) => (r.assetClass === AssetClass.CASH ? 0 : 1);
  return rows.sort(
    (a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label),
  );
}
