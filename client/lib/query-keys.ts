import type { QueryClient } from "@tanstack/react-query";
import type { RunFilters } from "./admin";

// Query keys, in one place so invalidation and the queries it targets cannot
// drift apart. The first element is the resource name, so invalidateQueries
// prefix-matches every variant of it, and every parameter is a primitive,
// because keys are compared structurally and a rebuilt object would be a new
// key that silently refetches.
export const qk = {
  session: () => ["session"] as const,
  statements: () => ["statements"] as const,
  statement: (id: string) => ["statements", id] as const,
  // Under a resource of its own, so refreshing the statements after a
  // choice does not run the resolution again.
  candidates: (keyId: string) => ["candidates", keyId] as const,
  run: (id: string) => ["runs", id] as const,
  transactions: () => ["transactions"] as const,
  holdings: () => ["holdings"] as const,
  instruments: () => ["instruments"] as const,
  adminRuns: (f: RunFilters) =>
    ["admin-runs", f.kind, f.trigger, f.state, f.user, f.before] as const,
  adminRun: (id: string) => ["admin-runs", id] as const,
  adminRunItems: (id: string) => ["admin-runs", id, "items"] as const,
  adminRunsAll: () => ["admin-runs"] as const,
  datasources: () => ["datasources"] as const,
  blocks: (cleared: boolean, before: string) =>
    ["blocks", cleared, before] as const,
  blocksAll: () => ["blocks"] as const,
};

// invalidateCleared refreshes the listings that clearing a finding or a
// block affects.
export function invalidateCleared(queryClient: QueryClient) {
  return Promise.all(
    [qk.blocksAll(), qk.adminRunsAll()].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}
