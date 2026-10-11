import { type Identifier, IdentifierType } from "@/gen/type/v1/type_pb";

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

// An IdentifierGroup is the identifiers of one type and value, with their
// domains.
export type IdentifierGroup = {
  type: IdentifierType;
  value: string;
  domains: string[];
  // True when a stated key's via identifier is in the group.
  via: boolean;
  // True when the identifier naming the instrument is in the group.
  name: boolean;
};

// identifierKey names an identifier for set membership.
export function identifierKey(id: Identifier): string {
  return `${id.type}\u0000${id.domain}\u0000${id.value}`;
}

// groupIdentifiers gathers identifiers into one group per type and value,
// listing the domains each appears in. Types keep the API's naming order,
// and a group in marks.via sorts first within its type. marks.via and
// marks.name hold identifierKey values and flag the groups they match.
export function groupIdentifiers(
  ids: Identifier[],
  marks: { via?: Set<string>; name?: Set<string> } = {},
): IdentifierGroup[] {
  const groups = new Map<string, IdentifierGroup>();
  for (const id of ids) {
    const key = `${id.type}\u0000${id.value}`;
    let g = groups.get(key);
    if (!g) {
      g = {
        type: id.type,
        value: id.value,
        domains: [],
        via: false,
        name: false,
      };
      groups.set(key, g);
    }
    if (id.domain) {
      g.domains.push(id.domain);
    }
    const k = identifierKey(id);
    g.via ||= marks.via?.has(k) ?? false;
    g.name ||= marks.name?.has(k) ?? false;
  }
  const order = new Map<IdentifierType, number>();
  for (const g of groups.values()) {
    if (!order.has(g.type)) order.set(g.type, order.size);
  }
  const rank = (t: IdentifierType) => order.get(t) ?? 0;
  return [...groups.values()].sort(
    (a, b) =>
      rank(a.type) - rank(b.type) ||
      Number(b.via) - Number(a.via) ||
      a.value.localeCompare(b.value),
  );
}
