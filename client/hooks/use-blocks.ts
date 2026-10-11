"use client";

import {
  type UseQueryResult,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListBlocksResponse } from "@/gen/admin/block/v1/block_pb";
import type { ListParams } from "@/lib/admin";
import { invalidateCleared, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useBlocks lists datasource blocks, newest first.
export function useBlocks(p: ListParams): UseQueryResult<ListBlocksResponse> {
  const { block } = useClients();
  return useAuthedQuery({
    queryKey: qk.blocks(p.cleared, p.before),
    queryFn: () =>
      block.listBlocks({ includeCleared: p.cleared, pageToken: p.before }),
  });
}

// useClearBlock clears a block and the finding reporting it.
export function useClearBlock() {
  const { block } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => block.clearBlock({ blockId: id }),
    onSuccess: () => invalidateCleared(queryClient),
  });
}
