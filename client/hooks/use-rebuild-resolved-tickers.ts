"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartRebuildResolvedTickers starts a rebuild resolved tickers run over
// every ticker. The runs listing reloads to show the new run.
export function useStartRebuildResolvedTickers() {
  const { rebuildResolvedTickers } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => rebuildResolvedTickers.startRebuildResolvedTickers({}),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
