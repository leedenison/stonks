import {
  AssetClass,
  type Identifier,
  IdentifierType,
} from "@/gen/type/v1/type_pb";

// preferred names the identifier types that label an instrument, the one a
// holder recognises most readily first. A ticker stated without its venue
// names no listing, but it is still the name the holder uses, so it is
// eligible.
export const preferred: IdentifierType[] = [
  IdentifierType.MIC_TICKER,
  IdentifierType.BROKER_DESCRIPTION,
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

// leading names the types that identify a class before any other, for the
// classes that have them. A datasource's OCC symbol outranks the symbol a
// broker's terms build.
const leading: Partial<Record<AssetClass, IdentifierType[]>> = {
  [AssetClass.CASH]: [IdentifierType.CURRENCY],
  [AssetClass.OPTION]: [IdentifierType.OCC, IdentifierType.OPTION],
  [AssetClass.FUTURE]: [IdentifierType.OCC, IdentifierType.OPTION],
};

// pick returns the first identifier of the first type in order that has
// one. Of two identifiers of one type, such as a ticker at two venues, the
// first the API lists wins.
export function pick(
  order: IdentifierType[],
  identifiers: Identifier[],
): Identifier | undefined {
  for (const type of order) {
    const found = identifiers.find((i) => i.type === type);
    if (found) return found;
  }
  return undefined;
}

// nameOf returns the identifier naming an instrument of assetClass as the
// user knows it, by its class's leading types, then preferred.
export function nameOf(
  assetClass: AssetClass,
  identifiers: Identifier[],
): Identifier | undefined {
  return pick([...(leading[assetClass] ?? []), ...preferred], identifiers);
}
