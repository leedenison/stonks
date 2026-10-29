"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetStmtResponse } from "@/gen/stmt/v1/stmt_pb";
import { qk } from "@/lib/query-keys";
import { pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useStmt reads one statement with every rejected row, polling while the
// statement is live.
export function useStmt(id: string): UseQueryResult<GetStmtResponse> {
  const { stmt } = useClients();
  return useAuthedQuery({
    queryKey: qk.stmt(id),
    queryFn: () => stmt.getStmt({ runId: id }),
    refetchInterval: (q) => (q.state.data?.stmt?.live ? pollInterval : false),
  });
}
