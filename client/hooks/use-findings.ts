"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { invalidateCleared } from "@/lib/query-keys";

// useClearFinding clears a finding that reports no block.
export function useClearFinding() {
  const { adminRun } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => adminRun.clearFinding({ findingId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}
