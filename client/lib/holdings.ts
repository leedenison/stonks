import type {
  Description,
  GroupHolding,
  InstrumentHolding,
  ListHoldingsResponse,
} from "@/gen/holding/v1/holding_pb";
import {
  AssetClass,
  type Identifier,
  IdentifierType,
} from "@/gen/type/v1/type_pb";
import { nameOf, pick, preferred } from "./identifiers";

// shown names the identifier types displayed beside a resolved holding's
// name, the registry codes that identify an instrument. The instruments page
// lists every other identifier under its listing, such as tickers at other
// venues and the codes of one datasource or broker.
const shown: IdentifierType[] = [
  IdentifierType.ISIN,
  IdentifierType.CUSIP,
  IdentifierType.SEDOL,
  IdentifierType.CINS,
  IdentifierType.WERTPAPIER,
  IdentifierType.OPENFIGI_COMPOSITE,
  IdentifierType.OPENFIGI_SHARE_CLASS,
];

// A HoldingRow is one line of the holdings table.
export type HoldingRow = {
  id: string;
  // The holding's name.
  label: string;
  classes: AssetClass[];
  quantity: string;
  // The identifiers shown beside the label, excluding the one that names
  // the holding.
  identifiers: Identifier[];
} & (
  | { kind: "instrument" }
  | {
      kind: "group";
      // The descriptions the keys state, excluding the one that names the
      // holding.
      descriptions: Description[];
    }
);

function instrumentRow(h: InstrumentHolding): HoldingRow {
  const name = nameOf(h.assetClass, h.identifiers);
  return {
    kind: "instrument",
    id: h.instrumentId,
    label: name?.value ?? h.instrumentId,
    classes: [h.assetClass],
    quantity: h.quantity,
    identifiers: h.identifiers.filter(
      (i) => i !== name && shown.includes(i.type),
    ),
  };
}

// groupRow names a group by what a user most readily recognises of a holding
// nothing has identified, which is a broker's description. Everything else
// its keys state is shown beside the name.
function groupRow(h: GroupHolding): HoldingRow {
  const described = h.descriptions[0];
  const name =
    described === undefined ? pick(preferred, h.identifiers) : undefined;
  return {
    kind: "group",
    id: h.groupId,
    label: described?.text ?? name?.value ?? h.groupId,
    classes: h.assetClasses,
    quantity: h.quantity,
    identifiers: h.identifiers.filter((i) => i !== name),
    descriptions: h.descriptions.filter((d) => d !== described),
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
