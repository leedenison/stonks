"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListHoldingsResponse } from "@/gen/holding/v1/holding_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useHoldings lists the caller's holdings. When a run reaches a terminal
// state, it invalidates the query, so the query does not poll.
export function useHoldings(): UseQueryResult<ListHoldingsResponse> {
  const { holding } = useClients();
  return useAuthedQuery({
    queryKey: qk.holdings(),
    queryFn: () => holding.listHoldings({}),
  });
}
