"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";

// useClearFinding clears a finding that reports no block.
export function useClearFinding() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => admin.clearFinding({ findingId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}

// invalidateCleared refreshes every listing a clear changes: the blocks and
// the runs whose findings are shown.
export function invalidateCleared(
  queryClient: ReturnType<typeof useQueryClient>,
) {
  return Promise.all(
    [["blocks"], ["admin-runs"]].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}
