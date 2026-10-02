"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { StartReplayRequestSchema } from "@/gen/admin/v1/admin_pb";
import { qk } from "@/lib/query-keys";

// useStartReplay starts a replay over the keys of a run and refreshes the
// runs, which now hold it.
export function useStartReplay() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof StartReplayRequestSchema>) =>
      admin.startReplay(req),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
