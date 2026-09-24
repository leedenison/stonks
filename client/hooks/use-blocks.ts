"use client";

import {
  type UseQueryResult,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListBlocksResponse } from "@/gen/admin/v1/admin_pb";
import type { ListParams } from "@/lib/admin";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";
import { invalidateCleared } from "./use-findings";

// useBlocks lists datasource blocks, newest first.
export function useBlocks(p: ListParams): UseQueryResult<ListBlocksResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.blocks(p.cleared, p.before),
    queryFn: () =>
      admin.listBlocks({ includeCleared: p.cleared, pageToken: p.before }),
  });
}

// useClearBlock clears a block and the finding reporting it.
export function useClearBlock() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => admin.clearBlock({ blockId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}
