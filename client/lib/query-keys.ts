import type { QueryClient } from "@tanstack/react-query";
import type { RunKind } from "@/gen/run/v1/run_pb";
import type { RunFilters } from "./admin";

// Query keys, in one place so invalidation and the queries it targets cannot
// drift apart. The first element is the resource name, so invalidateQueries
// prefix-matches every variant of it, and every parameter is a primitive,
// because keys are compared structurally and a rebuilt object would be a new
// key that silently refetches.
export const qk = {
  session: () => ["session"] as const,
  stmts: () => ["stmts"] as const,
  stmt: (id: string) => ["stmts", id] as const,
  // Under a resource of its own, so refreshing the statements after a
  // choice does not run the resolution again.
  candidates: (keyId: string) => ["candidates", keyId] as const,
  run: (id: string) => ["runs", id] as const,
  txns: () => ["txns"] as const,
  holdings: () => ["holdings"] as const,
  adminInstrs: (after: string, q: string, cls: string) =>
    ["admin-instrs", after, q, cls] as const,
  adminRuns: (f: RunFilters) =>
    ["admin-runs", f.kind, f.trigger, f.state, f.user, f.before] as const,
  adminRun: (id: string) => ["admin-runs", id] as const,
  adminRunItems: (id: string) => ["admin-runs", id, "items"] as const,
  adminLastRun: (kind: RunKind) => ["admin-runs", "last", kind] as const,
  adminRunsAll: () => ["admin-runs"] as const,
  datasources: () => ["datasources"] as const,
  blocks: (cleared: boolean, before: string) =>
    ["blocks", cleared, before] as const,
  alertDigest: () => ["alerts", "digest"] as const,
  alerts: (kind: number, token: string) =>
    ["alerts", "list", kind, token] as const,
  alertsAll: () => ["alerts"] as const,
  blocksAll: () => ["blocks"] as const,
  corpActs: (instrId: string) => ["corp-acts", "instr", instrId] as const,
  corpAct: (id: string) => ["corp-acts", id] as const,
  corpActsAll: () => ["corp-acts"] as const,
  instrTickers: (instrId: string) => ["coverage", "instr", instrId] as const,
  coverageTicker: (market: string, value: string) =>
    ["coverage", "ticker", market, value] as const,
  coverageAll: () => ["coverage"] as const,
};

// invalidateResolved refreshes the holdings, the transactions and the
// statements that a resolution changes. It refreshes the statements in
// stmtIds, or every statement and the statements list when the caller
// passes none.
export function invalidateResolved(
  queryClient: QueryClient,
  stmtIds?: string[],
) {
  const stmts = stmtIds?.map((id) => qk.stmt(id)) ?? [qk.stmts()];
  return Promise.all(
    [qk.holdings(), qk.txns(), ...stmts].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

// invalidateCleared refreshes the listings that clearing a finding, a block
// or a run affects, including the alerts.
export function invalidateCleared(queryClient: QueryClient) {
  return Promise.all(
    [qk.blocksAll(), qk.adminRunsAll(), qk.alertsAll()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

// invalidateDecided refreshes every query a decision on a corporate action
// can change: the actions, the holdings they restate, and the runs whose
// finding the decision cleared.
export function invalidateDecided(queryClient: QueryClient) {
  return Promise.all(
    [qk.corpActsAll(), qk.holdings(), qk.adminRunsAll(), qk.alertsAll()].map(
      (queryKey) => queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

// invalidateAttested refreshes every query an identifier attestation or a
// withdrawal can change.
export function invalidateAttested(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: qk.coverageAll() }),
    queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
    queryClient.invalidateQueries({ queryKey: qk.alertsAll() }),
    invalidateResolved(queryClient),
  ]);
}
