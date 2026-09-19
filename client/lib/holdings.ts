import type { Holding } from "@/gen/holding/v1/holding_pb";
import { AssetClass, IdentifierType } from "@/gen/type/v1/type_pb";

// holdingLabel names a holding as the user knows it: the currency of cash,
// else the broker's description of the line, else the first identifier,
// else the instrument id.
export function holdingLabel(holding: Holding): string {
  const of = (type: IdentifierType) =>
    holding.identifiers.find((i) => i.type === type)?.value;
  const named =
    holding.assetClass === AssetClass.CASH
      ? of(IdentifierType.CURRENCY)
      : of(IdentifierType.BROKER_DESCRIPTION);
  return named ?? holding.identifiers[0]?.value ?? holding.instrumentId;
}

// sortHoldings returns a copy with cash first, and each group by label.
export function sortHoldings(holdings: Holding[]): Holding[] {
  const rank = (h: Holding) => (h.assetClass === AssetClass.CASH ? 0 : 1);
  return [...holdings].sort(
    (a, b) =>
      rank(a) - rank(b) || holdingLabel(a).localeCompare(holdingLabel(b)),
  );
}
