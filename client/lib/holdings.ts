import type {
  GroupHolding,
  InstrumentHolding,
  ListHoldingsResponse,
} from "@/gen/holding/v1/holding_pb";
import { AssetClass, IdentifierType } from "@/gen/type/v1/type_pb";

// preferred names the identifier types a holding is labelled by, the one a
// holder recognises most readily first: a ticker, then the registry codes by
// how widely they are quoted, then the codes only their issuer reads. A
// ticker stated without its venue names nothing, and is still the string a
// holder knows the line by, so it is eligible here.
const preferred: IdentifierType[] = [
  IdentifierType.MIC_TICKER,
  IdentifierType.DATASOURCE_TICKER,
  IdentifierType.ISIN,
  IdentifierType.CUSIP,
  IdentifierType.SEDOL,
  IdentifierType.CINS,
  IdentifierType.WERTPAPIER,
  IdentifierType.OPENFIGI_COMPOSITE,
  IdentifierType.OPENFIGI_TICKER,
  IdentifierType.OPENFIGI_SHARE_CLASS,
  IdentifierType.BROKER_ID,
];

// leading names the type a class is known by ahead of any other, for the
// classes that have one. A class not named here takes preferred alone, and
// every class takes preferred after its own.
const leading: Partial<Record<AssetClass, IdentifierType>> = {
  [AssetClass.CASH]: IdentifierType.CURRENCY,
  [AssetClass.OPTION]: IdentifierType.OCC,
  [AssetClass.FUTURE]: IdentifierType.OCC,
};

// instrumentLabel names an instrument holding as the user knows it: the
// first identifier it holds of a type its class prefers, else the instrument
// id. Where it holds two of one type, such as a ticker at two venues, the
// first the API lists wins.
function instrumentLabel(holding: InstrumentHolding): string {
  const lead = leading[holding.assetClass];
  const order = lead === undefined ? preferred : [lead, ...preferred];
  for (const type of order) {
    const found = holding.identifiers.find((i) => i.type === type);
    if (found) return found.value;
  }
  return holding.instrumentId;
}

// groupLabel names a group holding by a description one of its brokers gave
// the line, which is the most a user recognises of a holding nothing has
// identified, else by an identifier its keys state, else by the group id.
function groupLabel(holding: GroupHolding): string {
  const described = holding.descriptions[0]?.text;
  if (described !== undefined) return described;
  for (const type of preferred) {
    const found = holding.identifiers.find((i) => i.type === type);
    if (found) return found.value;
  }
  return holding.groupId;
}

// HoldingRow is one line of the holdings table, of either kind.
export type HoldingRow = {
  id: string;
  label: string;
  classes: AssetClass[];
  quantity: string;
};

// holdingRows turns a response into the table's rows, cash first and the rest
// by label.
export function holdingRows(
  res: ListHoldingsResponse | undefined,
): HoldingRow[] {
  const rows: HoldingRow[] = [
    ...(res?.instruments ?? []).map((h) => ({
      id: h.instrumentId,
      label: instrumentLabel(h),
      classes: [h.assetClass],
      quantity: h.quantity,
    })),
    ...(res?.groups ?? []).map((h) => ({
      id: h.groupId,
      label: groupLabel(h),
      classes: h.assetClasses,
      quantity: h.quantity,
    })),
  ];
  const rank = (r: HoldingRow) =>
    r.classes.length === 1 && r.classes[0] === AssetClass.CASH ? 0 : 1;
  return rows.sort(
    (a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label),
  );
}
