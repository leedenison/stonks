"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListStmtsResponse } from "@/gen/stmt/v1/stmt_pb";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useStmts lists the caller's statements, newest first, and polls while
// any of their runs is live so a chip changes without a reload.
export function useStmts(): UseQueryResult<ListStmtsResponse> {
  const { stmt } = useClients();
  return useAuthedQuery({
    queryKey: qk.stmts(),
    queryFn: () => stmt.listStmts({}),
    refetchInterval: (q) =>
      anyLive((q.state.data?.stmts ?? []).map((s) => s.run?.state))
        ? pollInterval
        : false,
  });
}
