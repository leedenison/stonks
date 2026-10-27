"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  useMutation,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  AttestIdentifierRequestSchema,
  GetTickerResponse,
  ListInstrTickersResponse,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { invalidateAttested, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useInstrTickers lists the tickers that name an instrument, now or in
// an assertion.
export function useInstrTickers(
  instrId: string,
): UseQueryResult<ListInstrTickersResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.instrTickers(instrId),
    queryFn: () => coverage.listInstrTickers({ instrId }),
    enabled: instrId !== "",
  });
}

// useCoverageTicker reads the facts and ticker validity of a ticker at its
// market.
export function useCoverageTicker(
  market: string,
  value: string,
): UseQueryResult<GetTickerResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.coverageTicker(market, value),
    queryFn: () => coverage.getTicker({ ticker: { market, value } }),
    enabled: market !== "" && value !== "",
  });
}

// useAttestIdentifier writes an identifier attestation, as a system run.
export function useAttestIdentifier() {
  const { coverage } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof AttestIdentifierRequestSchema>) =>
      coverage.attestIdentifier(req),
    onSuccess: () => invalidateAttested(queryClient),
  });
}

// useWithdrawIdentifierAttestation deletes an attested assertion.
export function useWithdrawIdentifierAttestation() {
  const { coverage } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (assertionId: string) =>
      coverage.withdrawIdentifierAttestation({ assertionId }),
    onSuccess: () => invalidateAttested(queryClient),
  });
}
