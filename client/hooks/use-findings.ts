"use client";

import {
  type UseQueryResult,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListFindingsResponse } from "@/gen/admin/v1/admin_pb";
import type { ListParams } from "@/lib/admin";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useFindings lists findings across every run, newest first.
export function useFindings(
  p: ListParams,
): UseQueryResult<ListFindingsResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.findings(p.cleared, p.before),
    queryFn: () =>
      admin.listFindings({ includeCleared: p.cleared, pageToken: p.before }),
  });
}

// useClearFinding clears a finding that reports no block.
export function useClearFinding() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => admin.clearFinding({ findingId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}

// invalidateCleared refreshes every listing a clear changes: the findings,
// the blocks and the runs whose findings are shown.
export function invalidateCleared(
  queryClient: ReturnType<typeof useQueryClient>,
) {
  return Promise.all(
    [["findings"], ["blocks"], ["admin-runs"]].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}
