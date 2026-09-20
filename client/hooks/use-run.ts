"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetRunResponse } from "@/gen/run/v1/run_pb";
import { qk } from "@/lib/query-keys";
import { isTerminal, pollInterval } from "@/lib/run";
import { useAuthedQuery } from "./use-authed-query";

// useRun follows one run, polling until it reaches a terminal state.
export function useRun(id: string): UseQueryResult<GetRunResponse> {
  const { run } = useClients();
  return useAuthedQuery({
    queryKey: qk.run(id),
    queryFn: () => run.getRun({ runId: id }),
    refetchInterval: (q) =>
      isTerminal(q.state.data?.run?.state) ? false : pollInterval,
  });
}
