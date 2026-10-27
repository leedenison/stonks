"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartRefreshCorporateEvents starts a refresh corporate events run over
// the tickers that named a held instrument. The runs listing reloads to show
// the new run.
export function useStartRefreshCorporateEvents() {
  const { refreshCorporateEvents } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => refreshCorporateEvents.startRefreshCorporateEvents({}),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
