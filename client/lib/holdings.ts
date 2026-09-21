import type { Holding } from "@/gen/holding/v1/holding_pb";
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

// holdingLabel names a holding as the user knows it: the first identifier it
// holds of a type its class prefers, else the instrument id. Where it holds
// two of one type, such as a ticker at two venues, the first the API lists
// wins.
export function holdingLabel(holding: Holding): string {
  const lead = leading[holding.assetClass];
  const order = lead === undefined ? preferred : [lead, ...preferred];
  for (const type of order) {
    const found = holding.identifiers.find((i) => i.type === type);
    if (found) return found.value;
  }
  return holding.instrumentId;
}

// sortHoldings returns a copy with cash first, and each group by label.
export function sortHoldings(holdings: Holding[]): Holding[] {
  const rank = (h: Holding) => (h.assetClass === AssetClass.CASH ? 0 : 1);
  return [...holdings].sort(
    (a, b) =>
      rank(a) - rank(b) || holdingLabel(a).localeCompare(holdingLabel(b)),
  );
}
