"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartExtendValidity starts a reassert tickers run over every held ticker
// and refreshes the runs listing, which gains the new run.
export function useStartExtendValidity() {
  const { extendValidity } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => extendValidity.startExtendValidity({}),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
