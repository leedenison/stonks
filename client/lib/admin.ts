import {
  DropStep,
  type Finding,
  type UserRun,
} from "@/gen/admin/run/v1/run_pb";
import { type Run, RunKind, RunState, RunTrigger } from "@/gen/run/v1/run_pb";
import { TickerRelation } from "@/gen/type/v1/type_pb";
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
  return `${enumLabel(RunKind, run.kind)}${at ? ` @ ${at}` : ""}`;
}

// runTitle names a listed run: a datasource fetch run by the host it called,
// and any other run as runLabel does.
export function runTitle(r: UserRun): string {
  if (r.fetch) {
    try {
      return new URL(r.fetch.endpoint).host;
    } catch {
      return r.fetch.endpoint;
    }
  }
  return runLabel(r.run);
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

// InstrParams is the instruments page's address.
export type InstrParams = {
  // The token of the page, as the API returned it; empty for the first page.
  after: string;
  // The selected instrument; empty for none.
  id: string;
  // The search text; empty for none.
  q: string;
  // The asset class as enumParam writes it; empty for any.
  class: string;
  // The pane's tab: "tickers", or empty for the overview.
  tab: string;
  // The ticker the tickers tab shows, as tickerParam writes it; empty for
  // the first listed.
  ticker: string;
};

export function readInstrList(params: URLSearchParams): InstrParams {
  return {
    after: params.get("after") ?? "",
    id: params.get("id") ?? "",
    q: params.get("q") ?? "",
    class: params.get("class") ?? "",
    tab: params.get("tab") ?? "",
    ticker: params.get("ticker") ?? "",
  };
}

// relationSuffix marks each ticker that no longer names the instrument.
export const relationSuffix: Record<TickerRelation, string> = {
  [TickerRelation.UNSPECIFIED]: "",
  [TickerRelation.CURRENT]: "",
  [TickerRelation.DEFUNCT]: " (defunct)",
  [TickerRelation.FORMER]: " (former)",
};

// tickerParam writes a ticker for the instruments page's address, at its
// market or at one of the market's venues.
export function tickerParam(at: string, value: string): string {
  return `${at}:${value}`;
}

// readTickerParam reads what tickerParam wrote, or undefined when the text
// is not one.
export function readTickerParam(
  s: string,
): { at: string; value: string } | undefined {
  const i = s.indexOf(":");
  if (i <= 0 || i === s.length - 1) {
    return undefined;
  }
  return { at: s.slice(0, i), value: s.slice(i + 1) };
}

// instrQuery is the address of the instruments page with p applied.
export function instrQuery(p: InstrParams): string {
  return queryHref("/admin/instrs", p);
}

// flattenRuns lists every run of the trees, each parent before its children.
export function flattenRuns(runs: UserRun[]): UserRun[] {
  return runs.flatMap((r) => [r, ...flattenRuns(r.children)]);
}

// openFindingsBelow sums the open findings of r and every run under it.
export function openFindingsBelow(r: UserRun): number {
  return r.children.reduce((n, c) => n + openFindingsBelow(c), r.openFindings);
}
