"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartRefreshCorpActs starts a refresh corporate actions run over
// the tickers that named a held instrument. The runs listing reloads to show
// the new run.
export function useStartRefreshCorpActs() {
  const { refreshCorpActs } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => refreshCorpActs.startRefreshCorpActs({}),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
