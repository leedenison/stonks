"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartRebuildTickerValidity starts a rebuild ticker validity run over
// every ticker. The runs listing reloads to show the new run.
export function useStartRebuildTickerValidity() {
  const { rebuildTickerValidity } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => rebuildTickerValidity.startRebuildTickerValidity({}),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
