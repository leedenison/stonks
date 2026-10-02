import {
  type Identifier,
  IdentifierType,
  type StatedKey,
} from "@/gen/type/v1/type_pb";
import { enumLabel } from "@/lib/admin";
import { Chip } from "./chip";

// The short name each identifier type is shown under.
const labels: Record<number, string> = {
  [IdentifierType.ISIN]: "ISIN",
  [IdentifierType.CUSIP]: "CUSIP",
  [IdentifierType.CINS]: "CINS",
  [IdentifierType.WERTPAPIER]: "WKN",
  [IdentifierType.OPENFIGI_SHARE_CLASS]: "FIGI share class",
  [IdentifierType.SEDOL]: "SEDOL",
  [IdentifierType.OPENFIGI_COMPOSITE]: "FIGI composite",
  [IdentifierType.MIC_TICKER]: "Ticker",
  [IdentifierType.OPENFIGI_TICKER]: "FIGI ticker",
  [IdentifierType.OCC]: "OCC",
  [IdentifierType.CURRENCY]: "Currency",
  [IdentifierType.DATASOURCE_TICKER]: "Source ticker",
  [IdentifierType.BROKER_ID]: "Broker id",
};

export function identifierLabel(type: IdentifierType): string {
  return labels[type] ?? enumLabel(IdentifierType, type);
}

// IdentifierChip is one identifier: its type, its value, and its domain in
// parentheses where it has one.
export function IdentifierChip({ id }: { id: Identifier }) {
  return (
    <Chip
      tone="primary"
      className="font-mono"
      data-identifier-type={IdentifierType[id.type]}
    >
      <span className="font-semibold">{identifierLabel(id.type)}</span>
      <span>{id.value}</span>
      {id.domain && <span className="opacity-70">({id.domain})</span>}
    </Chip>
  );
}

// IdentifierChips is a chip per identifier, wrapping.
export function IdentifierChips({ ids }: { ids: Identifier[] }) {
  return (
    <span className="flex flex-wrap gap-1">
      {ids.map((id) => (
        <IdentifierChip key={`${id.type}-${id.domain}-${id.value}`} id={id} />
      ))}
    </span>
  );
}

// StatedKeyChips is what a stated key states: its identifiers, then its
// description.
export function StatedKeyChips({ statedKey }: { statedKey?: StatedKey }) {
  if (!statedKey) return null;
  return (
    <span className="flex flex-wrap items-center gap-1">
      <IdentifierChips ids={statedKey.identifiers} />
      {statedKey.description && <span>{statedKey.description}</span>}
    </span>
  );
}
