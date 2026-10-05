import { DropStep, type Finding, type UserRun } from "@/gen/admin/v1/admin_pb";
import { type Run, RunKind, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { enumLabel } from "./enum";
import { formatInstant } from "./format";

// RunFilters is the runs page's filters, as held in its address. An
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

// queryHref returns path with a query string of the params that have a
// value.
export function queryHref(
  path: string,
  params: Record<string, string>,
): string {
  const q = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== ""),
  ).toString();
  return q ? `${path}?${q}` : path;
}

// filterQuery is the address of the runs page with f applied.
export function filterQuery(f: Partial<RunFilters>): string {
  const params: Record<string, string> = {};
  for (const k of filterKeys) {
    params[k] = f[k] ?? "";
  }
  return queryHref("/admin/runs", params);
}

// runLabel names a run by its kind and when it started.
export function runLabel(run: Run | undefined): string {
  if (!run) {
    return "run";
  }
  const at = formatInstant(run.createdAt);
  return `${enumLabel(RunKind, run.kind)} run${at ? ` @ ${at}` : ""}`;
}

export const runEnums = {
  kind: RunKind,
  trigger: RunTrigger,
  state: RunState,
} as const;

// findingText renders what a finding says: the step that dropped a candidate
// group where there is one, then the grounds.
export function findingText(f: Finding): string {
  const parts: string[] = [];
  if (f.step !== undefined) parts.push(enumLabel(DropStep, f.step));
  if (f.detail) parts.push(f.detail);
  return parts.join(": ");
}

// ListParams is the blocks page's filters, as held in its address.
export type ListParams = { cleared: boolean; before: string };

export function readList(params: URLSearchParams): ListParams {
  return {
    cleared: params.get("cleared") === "1",
    before: params.get("before") ?? "",
  };
}

// listQuery is the address of the page at path with p applied.
export function listQuery(path: string, p: ListParams): string {
  return queryHref(path, { cleared: p.cleared ? "1" : "", before: p.before });
}

// flattenRuns lists every run of the trees, each parent before its children.
export function flattenRuns(runs: UserRun[]): UserRun[] {
  return runs.flatMap((r) => [r, ...flattenRuns(r.children)]);
}

// openFindingsBelow sums the open findings of r and every run under it.
export function openFindingsBelow(r: UserRun): number {
  return r.children.reduce((n, c) => n + openFindingsBelow(c), r.openFindings);
}
