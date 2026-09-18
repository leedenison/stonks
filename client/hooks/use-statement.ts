"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetStatementResponse } from "@/gen/statement/v1/statement_pb";
import { qk } from "@/lib/query-keys";
import { isTerminal, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useStatement reads one statement with every rejected row, polling while
// its run is live. The items are written with the run's completion, so
// nothing is re-downloaded until there is something to show.
export function useStatement(
  id: string,
  interval = pollInterval,
): UseQueryResult<GetStatementResponse> {
  const { statement } = useClients();
  return useAuthedQuery({
    queryKey: qk.statement(id),
    queryFn: () => statement.getStatement({ runId: id }),
    refetchInterval: (q) =>
      q.state.data && !isTerminal(q.state.data.statement?.run?.state)
        ? interval
        : false,
  });
}
