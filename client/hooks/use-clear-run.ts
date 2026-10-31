"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import { invalidateCleared } from "@/lib/query-keys";

// useClearRun clears a run that failed or was interrupted.
export function useClearRun() {
  const { adminRun } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => adminRun.clearRun({ runId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}
