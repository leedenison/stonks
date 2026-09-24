"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListRunsResponse } from "@/gen/admin/v1/admin_pb";
import { fromParam, type RunFilters, runEnums } from "@/lib/admin";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useAdminRuns lists every user's runs matching f, newest first, polling
// while any listed run is live.
export function useAdminRuns(f: RunFilters): UseQueryResult<ListRunsResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminRuns(f),
    queryFn: () =>
      admin.listRuns({
        kind: fromParam(runEnums.kind, f.kind),
        trigger: fromParam(runEnums.trigger, f.trigger),
        state: fromParam(runEnums.state, f.state),
        userId: f.user || undefined,
        pageToken: f.before,
      }),
    refetchInterval: (q) =>
      anyLive((q.state.data?.runs ?? []).map((r) => r.run?.state))
        ? pollInterval
        : false,
  });
}
