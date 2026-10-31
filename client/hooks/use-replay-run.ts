"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { StartReplayRunRequestSchema } from "@/gen/admin/replay_run/v1/replay_run_pb";
import { qk } from "@/lib/query-keys";

// useStartReplayRun starts a replay over the keys of a run and refreshes the
// runs, which now hold it.
export function useStartReplayRun() {
  const { replayRun } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof StartReplayRunRequestSchema>) =>
      replayRun.startReplayRun(req),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
  });
}
