"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { RunKind } from "@/gen/run/v1/run_pb";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";

// useStartSystemRun starts a system run of the kind. The runs listing
// reloads to show the new run.
export function useStartSystemRun(kind: RunKind) {
  const { adminRun } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => adminRun.startSystemRun({ kind }),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
        queryClient.invalidateQueries({ queryKey: qk.alertsAll() }),
      ]),
  });
}
