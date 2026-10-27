import type { StmtItem } from "@/gen/stmt/v1/stmt_pb";
import {
  AssetClass,
  IdentifierType,
  type StatedKey,
} from "@/gen/type/v1/type_pb";

export type Group = { reason: string; items: StmtItem[] };

// groupByReason gathers rejected rows under their reason, in the order each
// reason first appears, so hundreds of rows sharing one reason read as one
// line with a count.
export function groupByReason(items: StmtItem[]): Group[] {
  const groups = new Map<string, Group>();
  for (const item of items) {
    let g = groups.get(item.reason);
    if (!g) {
      g = { reason: item.reason, items: [] };
      groups.set(item.reason, g);
    }
    g.items.push(item);
  }
  return [...groups.values()];
}

// keyLabel names a stated key as the export named it.
export function keyLabel(key: StatedKey | undefined): string {
  if (!key) {
    return "";
  }
  const description = key.identifiers.find(
    (i) => i.type === IdentifierType.BROKER_DESCRIPTION,
  );
  if (description) {
    return description.value;
  }
  if (key.assetClass === AssetClass.CASH && key.currency) {
    return key.currency;
  }
  return key.identifiers[0]?.value ?? "";
}
