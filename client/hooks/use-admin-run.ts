"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetRunResponse } from "@/gen/admin/v1/admin_pb";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useAdminRun reads any user's run with its children, findings and items,
// polling while it or a child is live.
export function useAdminRun(id: string): UseQueryResult<GetRunResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminRun(id),
    queryFn: () => admin.getRun({ runId: id }),
    refetchInterval: (q) => {
      const d = q.state.data;
      return d &&
        anyLive([d.run?.run?.state, ...d.children.map((c) => c.state)])
        ? pollInterval
        : false;
    },
  });
}
