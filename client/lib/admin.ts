import { RunKind, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import {
  type Identifier,
  IdentifierType,
  type StatedKey,
} from "@/gen/type/v1/type_pb";

// RunFilters is what the runs page lists by, as held in its address. An
// empty string matches everything.
export type RunFilters = {
  kind: string;
  trigger: string;
  state: string;
  user: string;
  before: string;
};

const filterKeys = ["kind", "trigger", "state", "user", "before"] as const;

export function readFilters(params: URLSearchParams): RunFilters {
  const f = {} as RunFilters;
  for (const k of filterKeys) {
    f[k] = params.get(k) ?? "";
  }
  return f;
}

// filterQuery is the address of the runs page with f applied.
export function filterQuery(f: RunFilters): string {
  const params = new URLSearchParams();
  for (const k of filterKeys) {
    if (f[k]) params.set(k, f[k]);
  }
  const q = params.toString();
  return q ? `/admin/runs?${q}` : "/admin/runs";
}

// enumLabel renders a generated enum value as lower case words, as
// FAILED_PERMANENT to "failed permanent".
export function enumLabel(e: Record<number, string>, v: number): string {
  return (e[v] ?? "").toLowerCase().replaceAll("_", " ");
}

// The enum values a filter offers, UNSPECIFIED left out.
export function enumValues(e: Record<number, string>): number[] {
  return Object.keys(e)
    .map(Number)
    .filter((v) => !Number.isNaN(v) && v !== 0);
}

export const runEnums = {
  kind: RunKind,
  trigger: RunTrigger,
  state: RunState,
} as const;

// identifierText renders an identifier as its type, its domain where it has
// one, and its value.
export function identifierText(i: Identifier): string {
  const type = IdentifierType[i.type] ?? "";
  return `${type} ${i.domain ? `${i.domain}:` : ""}${i.value}`;
}

// keyText renders what a stated key states: its identifiers, then its
// description.
export function keyText(k: StatedKey | undefined): string {
  if (!k) return "";
  const parts = k.identifiers.map(identifierText);
  if (k.description) parts.push(k.description);
  return parts.join(" · ");
}

// enumParam and fromParam carry an enum value in an address as its lower
// case name, as RunKind.STATEMENT to "statement".
export function enumParam(e: Record<number, string>, v: number): string {
  return (e[v] ?? "").toLowerCase();
}

export function fromParam(
  e: Record<string, string | number>,
  s: string,
): number | undefined {
  const v = e[s.toUpperCase()];
  return typeof v === "number" && v !== 0 ? v : undefined;
}

// ListParams is what the findings and blocks pages list by, as held in
// their address.
export type ListParams = { cleared: boolean; before: string };

export function readList(params: URLSearchParams): ListParams {
  return {
    cleared: params.get("cleared") === "1",
    before: params.get("before") ?? "",
  };
}

// listQuery is the address of the page at path with p applied.
export function listQuery(path: string, p: ListParams): string {
  const params = new URLSearchParams();
  if (p.cleared) params.set("cleared", "1");
  if (p.before) params.set("before", p.before);
  const q = params.toString();
  return q ? `${path}?${q}` : path;
}
