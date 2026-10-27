"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  useMutation,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  ConfirmCandidateRequestSchema,
  FindCandidatesResponse,
} from "@/gen/stmt/v1/stmt_pb";
import { invalidateResolved, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useCandidates finds the candidates for a key. Each read is a resolution,
// so an answer is never fresh and the focus of the window does not read
// again.
export function useCandidates(
  keyId: string,
): UseQueryResult<FindCandidatesResponse> {
  const { stmt } = useClients();
  return useAuthedQuery({
    queryKey: qk.candidates(keyId),
    queryFn: () => stmt.findCandidates({ statedKeyId: keyId }),
    staleTime: 0,
    refetchOnWindowFocus: false,
  });
}

// useChooseCandidate confirms a candidate for a key, then refreshes the
// statements and the holdings that include the key.
export function useChooseCandidate() {
  const { stmt } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof ConfirmCandidateRequestSchema>) =>
      stmt.confirmCandidate(req),
    onSuccess: () => invalidateResolved(queryClient),
  });
}
