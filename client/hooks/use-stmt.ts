"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetStmtResponse } from "@/gen/stmt/v1/stmt_pb";
import { qk } from "@/lib/query-keys";
import { isTerminal, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useStmt reads one statement with every rejected row, polling while
// its run is live. The items are written with the run's completion, so
// nothing is re-downloaded until there is something to show.
export function useStmt(id: string): UseQueryResult<GetStmtResponse> {
  const { stmt } = useClients();
  return useAuthedQuery({
    queryKey: qk.stmt(id),
    queryFn: () => stmt.getStmt({ runId: id }),
    refetchInterval: (q) =>
      q.state.data && !isTerminal(q.state.data.stmt?.run?.state)
        ? pollInterval
        : false,
  });
}
