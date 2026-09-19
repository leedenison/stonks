"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListHoldingsResponse } from "@/gen/holding/v1/holding_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useHoldings lists the caller's holdings. A run reaching a terminal state
// invalidates the query, so it does not poll.
export function useHoldings(): UseQueryResult<ListHoldingsResponse> {
  const { holding } = useClients();
  return useAuthedQuery({
    queryKey: qk.holdings(),
    queryFn: () => holding.listHoldings({}),
  });
}
