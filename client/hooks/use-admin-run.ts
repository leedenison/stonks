"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetRunResponse } from "@/gen/admin/run/v1/run_pb";
import { flattenRuns } from "@/lib/admin";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";
import { useRunSettled } from "./use-run-settled";

// live reports whether the run or a run below it is live.
function live(d: GetRunResponse | undefined): boolean | undefined {
  return (
    d && anyLive(flattenRuns(d.run ? [d.run] : []).map((r) => r.run?.state))
  );
}

// useAdminRun reads any user's run with the runs above and below it, its
// findings and its items, polling while it or a run below it is live.
export function useAdminRun(id: string): UseQueryResult<GetRunResponse> {
  const { adminRun } = useClients();
  const q = useAuthedQuery({
    queryKey: qk.adminRun(id),
    queryFn: () => adminRun.getRun({ runId: id }),
    refetchInterval: (q) => (live(q.state.data) ? pollInterval : false),
  });
  useRunSettled(live(q.data));
  return q;
}
