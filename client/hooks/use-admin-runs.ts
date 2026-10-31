"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListRunsResponse, UserRun } from "@/gen/admin/run/v1/run_pb";
import { type RunKind, RunTrigger } from "@/gen/run/v1/run_pb";
import { flattenRuns, type RunFilters, runEnums } from "@/lib/admin";
import { fromParam } from "@/lib/enum";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";
import { useRunSettled } from "./use-run-settled";

// live reports whether any listed run is live.
function live(d: ListRunsResponse | undefined): boolean | undefined {
  return d && anyLive(flattenRuns(d.runs).map((r) => r.run?.state));
}

// useAdminRuns lists every user's runs matching f, newest first, polling
// while any listed run is live.
export function useAdminRuns(f: RunFilters): UseQueryResult<ListRunsResponse> {
  const { adminRun } = useClients();
  const q = useAuthedQuery({
    queryKey: qk.adminRuns(f),
    queryFn: () =>
      adminRun.listRuns({
        kind: fromParam(runEnums.kind, f.kind),
        trigger: fromParam(runEnums.trigger, f.trigger),
        state: fromParam(runEnums.state, f.state),
        userId: f.user || undefined,
        pageToken: f.before,
      }),
    refetchInterval: (q) => (live(q.state.data) ? pollInterval : false),
  });
  useRunSettled(live(q.data));
  return q;
}

// useLastSystemRun reads the newest run of kind that an administrator
// started. It holds null when there is none. It polls while that run is
// live. A run of the kind that another run started does not count.
export function useLastSystemRun(
  kind: RunKind,
): UseQueryResult<UserRun | null> {
  const { adminRun } = useClients();
  const q = useAuthedQuery({
    queryKey: qk.adminLastRun(kind),
    queryFn: () =>
      adminRun.listRuns({
        kind,
        trigger: RunTrigger.ADMINISTRATOR,
        pageSize: 1,
      }),
    select: (res) => res.runs[0] ?? null,
    refetchInterval: (q) => (live(q.state.data) ? pollInterval : false),
  });
  useRunSettled(
    q.data === undefined ? undefined : anyLive([q.data?.run?.state]),
  );
  return q;
}
