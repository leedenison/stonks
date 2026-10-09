import type {
  GroupHolding,
  HoldingKey,
  HoldingListing,
  InstrumentHolding,
  ListHoldingsResponse,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  type Identifier,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { nameOf, pick } from "./identifiers";

// registry lists the identifier types a holding can show as its code, the
// most widely quoted first.
const registry: IdentifierType[] = [
  IdentifierType.ISIN,
  IdentifierType.CUSIP,
  IdentifierType.SEDOL,
  IdentifierType.WERTPAPIER,
  IdentifierType.CINS,
  IdentifierType.OPENFIGI_SHARE_CLASS,
];

// A HoldingRow is one line of the holdings table.
export type HoldingRow = {
  id: string;
  // The holding's name.
  label: string;
  // The common name of the venue where the label is a ticker; empty where
  // the label is not a ticker.
  venue: string;
  // The brokers' descriptions of the holding, other than the label.
  descriptions: string[];
  // The one registry code shown beside the label.
  code?: Identifier;
  // The currencies of the keys' listings, or of the keys' stated currency
  // for a group.
  currencies: string[];
  classes: AssetClass[];
  quantity: string;
  // The holding's registry codes.
  identifiers: Identifier[];
  listings: HoldingListing[];
  keys: HoldingKey[];
  kind: "instrument" | "group";
};

// descriptionsOf returns the distinct descriptions the brokers gave the
// keys, in the order of the keys.
function descriptionsOf(keys: HoldingKey[]): string[] {
  const out: string[] = [];
  for (const k of keys) {
    for (const i of k.statedKey?.identifiers ?? []) {
      if (
        i.type === IdentifierType.BROKER_DESCRIPTION &&
        !out.includes(i.value)
      ) {
        out.push(i.value);
      }
    }
  }
  return out;
}

// heldListing returns the listing that names the holding: the one the keys
// hold most of by absolute quantity, else the first listing.
function heldListing(h: InstrumentHolding): HoldingListing | undefined {
  const weight = new Map<string, number>();
  for (const k of h.keys) {
    if (k.listingId !== "") {
      weight.set(
        k.listingId,
        (weight.get(k.listingId) ?? 0) + Math.abs(Number(k.quantity)),
      );
    }
  }
  let best: HoldingListing | undefined;
  for (const l of h.listings) {
    const w = weight.get(l.id);
    if (w !== undefined && (best === undefined || w > weight.get(best.id)!)) {
      best = l;
    }
  }
  return best ?? h.listings[0];
}

function instrumentRow(h: InstrumentHolding): HoldingRow {
  const held = heldListing(h);
  const named = new Set(h.keys.map((k) => k.listingId).filter((id) => id));
  const currencies = h.listings
    .filter((l) => named.size === 0 || named.has(l.id))
    .map((l) => l.currency);
  const ticker = held?.ticker;
  return {
    kind: "instrument",
    id: h.instrumentId,
    label:
      ticker?.value ??
      nameOf(h.assetClass, h.identifiers)?.value ??
      h.instrumentId,
    venue: ticker ? (held?.venue ?? "") : "",
    descriptions: descriptionsOf(h.keys),
    code: pick(registry, h.identifiers),
    currencies,
    classes: [h.assetClass],
    quantity: h.quantity,
    identifiers: h.identifiers.filter((i) => registry.includes(i.type)),
    listings: h.listings,
    keys: h.keys,
  };
}

// groupRow names a group by the ticker its keys state, else by the first
// description a broker gave the line. The other descriptions sit beneath.
function groupRow(h: GroupHolding): HoldingRow {
  const name =
    pick([IdentifierType.MIC_TICKER], h.identifiers) ??
    pick([IdentifierType.BROKER_DESCRIPTION], h.identifiers);
  const currencies: string[] = [];
  for (const k of h.keys) {
    const c = k.statedKey?.currency;
    if (c !== undefined && !currencies.includes(c)) currencies.push(c);
  }
  return {
    kind: "group",
    id: h.groupId,
    label: name?.value ?? h.groupId,
    venue: "",
    descriptions: h.identifiers
      .filter((i) => i.type === IdentifierType.BROKER_DESCRIPTION && i !== name)
      .map((i) => i.value),
    code: pick(registry, h.identifiers),
    currencies,
    classes: h.assetClasses,
    quantity: h.quantity,
    identifiers: h.identifiers.filter((i) => registry.includes(i.type)),
    listings: [],
    keys: h.keys,
  };
}

// holdingRows turns a response into the table's rows, cash first and the rest
// by label.
export function holdingRows(
  res: ListHoldingsResponse | undefined,
): HoldingRow[] {
  const rows: HoldingRow[] = [
    ...(res?.instruments ?? []).map(instrumentRow),
    ...(res?.groups ?? []).map(groupRow),
  ];
  const rank = (r: HoldingRow) =>
    r.classes.length === 1 && r.classes[0] === AssetClass.CASH ? 0 : 1;
  return rows.sort(
    (a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label),
  );
}
