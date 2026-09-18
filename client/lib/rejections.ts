import type { StatementItem } from "@/gen/statement/v1/statement_pb";
import { AssetClass, type StatedKey } from "@/gen/type/v1/type_pb";

export type Group = { reason: string; items: StatementItem[] };

// groupByReason gathers rejected rows under their reason, in the order each
// reason first appears, so hundreds of rows sharing one reason read as one
// line with a count.
export function groupByReason(items: StatementItem[]): Group[] {
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

// keyLabel names a stated key as the export named it: its description, or
// its currency for a cash key, or the first identifier it states.
export function keyLabel(key: StatedKey | undefined): string {
  if (!key) {
    return "";
  }
  if (key.description) {
    return key.description;
  }
  if (key.assetClass === AssetClass.CASH && key.currency) {
    return key.currency;
  }
  return key.identifiers[0]?.value ?? "";
}
