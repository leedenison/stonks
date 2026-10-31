"use client";

import {
  useMutation,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  CorpActDecision,
  GetCorpActResponse,
  ListInstrCorpActsResponse,
} from "@/gen/admin/corpact/v1/corpact_pb";
import { invalidateDecided, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useInstrCorpActs lists the stored corporate actions of an instrument.
export function useInstrCorpActs(
  instrId: string,
): UseQueryResult<ListInstrCorpActsResponse> {
  const { corpAct } = useClients();
  return useAuthedQuery({
    queryKey: qk.corpActs(instrId),
    queryFn: () => corpAct.listInstrCorpActs({ instrId }),
    enabled: instrId !== "",
  });
}

// useCorpAct reads one corporate action with the evidence for deciding it.
export function useCorpAct(id: string): UseQueryResult<GetCorpActResponse> {
  const { corpAct } = useClients();
  return useAuthedQuery({
    queryKey: qk.corpAct(id),
    queryFn: () => corpAct.getCorpAct({ corpActId: id }),
    enabled: id !== "",
  });
}

// useDecideCorpAct records the decision on a corporate action.
export function useDecideCorpAct() {
  const { corpAct } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: { corpActId: string; decision: CorpActDecision }) =>
      corpAct.decideCorpAct(req),
    onSuccess: () => invalidateDecided(queryClient),
  });
}
