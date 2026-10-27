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
  ListInstrumentTickersResponse,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { invalidateAttested, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useInstrumentTickers lists the tickers that name an instrument, now or in
// an assertion.
export function useInstrumentTickers(
  instrumentId: string,
): UseQueryResult<ListInstrumentTickersResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.instrumentTickers(instrumentId),
    queryFn: () => coverage.listInstrumentTickers({ instrumentId }),
    enabled: instrumentId !== "",
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
