"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetRunResponse } from "@/gen/admin/v1/admin_pb";
import { flattenRuns } from "@/lib/admin";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useAdminRun reads any user's run with the runs above and below it, its
// findings and its items, polling while it or a run below it is live.
export function useAdminRun(id: string): UseQueryResult<GetRunResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminRun(id),
    queryFn: () => admin.getRun({ runId: id }),
    refetchInterval: (q) => {
      const d = q.state.data;
      return d &&
        anyLive(flattenRuns(d.run ? [d.run] : []).map((r) => r.run?.state))
        ? pollInterval
        : false;
    },
  });
}
