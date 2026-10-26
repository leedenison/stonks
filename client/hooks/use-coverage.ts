"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  useMutation,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  AttestRequestSchema,
  GetTickerResponse,
  ListInstrumentTickersResponse,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { IdentifierType } from "@/gen/type/v1/type_pb";
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

// useCoverageTicker reads one ticker's facts and validity.
export function useCoverageTicker(
  domain: string,
  value: string,
): UseQueryResult<GetTickerResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.coverageTicker(domain, value),
    queryFn: () =>
      coverage.getTicker({
        ticker: { type: IdentifierType.MIC_TICKER, domain, value },
      }),
    enabled: domain !== "" && value !== "",
  });
}

// useAttest writes an attestation, as a system run.
export function useAttest() {
  const { coverage } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof AttestRequestSchema>) =>
      coverage.attest(req),
    onSuccess: () => invalidateAttested(queryClient),
  });
}

// useWithdrawAttestation deletes an attested assertion.
export function useWithdrawAttestation() {
  const { coverage } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (assertionId: string) =>
      coverage.withdrawAttestation({ assertionId }),
    onSuccess: () => invalidateAttested(queryClient),
  });
}
