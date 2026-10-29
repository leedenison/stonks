import {
  type GroupHolding,
  type HoldingKey,
  type HoldingListing,
  HoldingMark,
  type InstrHolding,
  type ListHoldingsResponse,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  type Identifier,
  IdentifierType,
  TickerRelation,
} from "@/gen/type/v1/type_pb";
import { relationSuffix } from "./admin";
import { pick } from "./identifiers";
import { prevDay } from "./marshal/date";

// registry lists the identifier types a holding shows as its codes, the
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
  // The common name of the venue where the label is a ticker, or the
  // ticker's market where its sources omitted the venue; empty where the label
  // is not a ticker.
  venue: string;
  // The brokers' descriptions of the holding, other than the label.
  descriptions: string[];
  // The currencies of the keys' listings, or of the keys' stated currency
  // for a group.
  currencies: string[];
  classes: AssetClass[];
  quantity: string;
  // The holding's registry codes.
  identifiers: Identifier[];
  kind: "instr" | "group";
  // Whether the quantity follows every corporate action known so far while an
  // action may be missing, because actions are not known for part of the
  // period since a transaction.
  provisional: boolean;
  // The day from which a provisional holding needs corporate actions, as
  // YYYY-MM-DD.
  provisionalFrom?: string;
  // The runs of days with unknown corporate actions, each with its last day.
  gaps: Gap[];
};

// A Gap is a run of days that the corporate actions source of the holding's
// market has not covered. Both ends are included, as YYYY-MM-DD.
export type Gap = { from: string; to: string };

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
function heldListing(h: InstrHolding): HoldingListing | undefined {
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
  let bestWeight = 0;
  for (const l of h.listings) {
    const w = weight.get(l.id);
    if (w !== undefined && (best === undefined || w > bestWeight)) {
      best = l;
      bestWeight = w;
    }
  }
  return best ?? h.listings[0];
}

// instrRow names a holding by the ticker of the listing the user holds,
// at its venue, falling back to the name the API gives the instrument. A
// ticker that now names another instrument is marked defunct. A former
// ticker shows plainly, so a delisted holding keeps its name.
function instrRow(h: InstrHolding): HoldingRow {
  const held = heldListing(h);
  const named = new Set(h.keys.map((k) => k.listingId).filter((id) => id));
  const currencies = h.listings
    .filter((l) => named.size === 0 || named.has(l.id))
    .map((l) => l.currency);
  const ticker = held?.ticker;
  return {
    kind: "instr",
    id: h.instrId,
    label: ticker
      ? ticker.value +
        (held?.relation === TickerRelation.DEFUNCT
          ? relationSuffix[TickerRelation.DEFUNCT]
          : "")
      : (h.name?.value ?? h.instrId),
    venue: ticker ? (held?.venue ?? ticker.market) : "",
    descriptions: descriptionsOf(h.keys),
    currencies,
    classes: [h.assetClass],
    quantity: h.quantity,
    identifiers: h.identifiers.filter((i) => registry.includes(i.type)),
    provisional: h.mark === HoldingMark.CORP_ACTS_PROVISIONAL,
    provisionalFrom: h.provisionalFrom,
    gaps: h.gaps.map((g) => ({
      from: g.uncoveredFrom,
      to: prevDay(g.uncoveredBefore),
    })),
  };
}

// groupRow names a group by the option symbol or the ticker its keys state,
// else by the first description a broker gave the line.
function groupRow(h: GroupHolding): HoldingRow {
  const name =
    pick([IdentifierType.OPTION, IdentifierType.MIC_TICKER], h.identifiers) ??
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
    currencies,
    classes: h.assetClasses,
    quantity: h.quantity,
    identifiers: h.identifiers.filter((i) => registry.includes(i.type)),
    provisional: false,
    gaps: [],
  };
}

// isCash reports whether a row holds cash alone.
export function isCash(r: HoldingRow): boolean {
  return r.classes.length === 1 && r.classes[0] === AssetClass.CASH;
}

// hasDetail reports whether a row opens: a row other than cash with a code,
// a description or a provisional quantity to show.
export function hasDetail(r: HoldingRow): boolean {
  return (
    !isCash(r) &&
    (r.identifiers.length > 0 || r.descriptions.length > 0 || r.provisional)
  );
}

// holdingRows turns a response into the table's rows, cash first and the rest
// by label.
export function holdingRows(
  res: ListHoldingsResponse | undefined,
): HoldingRow[] {
  const rows: HoldingRow[] = [
    ...(res?.instrs ?? []).map(instrRow),
    ...(res?.groups ?? []).map(groupRow),
  ];
  const rank = (r: HoldingRow) => (isCash(r) ? 0 : 1);
  return rows.sort(
    (a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label),
  );
}
