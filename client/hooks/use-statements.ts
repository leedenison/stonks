"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListStatementsResponse } from "@/gen/statement/v1/statement_pb";
import { qk } from "@/lib/query-keys";
import { anyLive, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useStatements lists the caller's statements, newest first, and polls while
// any of their runs is live so a chip changes without a reload.
export function useStatements(
  interval = pollInterval,
): UseQueryResult<ListStatementsResponse> {
  const { statement } = useClients();
  return useAuthedQuery({
    queryKey: qk.statements(),
    queryFn: () => statement.listStatements({}),
    refetchInterval: (q) =>
      anyLive((q.state.data?.statements ?? []).map((s) => s.run?.state))
        ? interval
        : false,
  });
}
