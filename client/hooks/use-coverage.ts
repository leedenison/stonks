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
  ListDomainsResponse,
  ListGapsResponse,
} from "@/gen/admin/coverage/v1/coverage_pb";
import { IdentifierType } from "@/gen/type/v1/type_pb";
import { invalidateResolved, qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useCoverageDomains lists the operating MICs that have tickers with
// transactions.
export function useCoverageDomains(): UseQueryResult<ListDomainsResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.coverageDomains(),
    queryFn: () => coverage.listDomains({}),
  });
}

// useCoverageGaps lists the gaps of the tickers stated on domain.
export function useCoverageGaps(
  domain: string,
): UseQueryResult<ListGapsResponse> {
  const { coverage } = useClients();
  return useAuthedQuery({
    queryKey: qk.coverageGaps(domain),
    queryFn: () => coverage.listGaps({ domain }),
    enabled: domain !== "",
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

// invalidateAttested refreshes every query an attestation or a withdrawal
// can change.
function invalidateAttested(queryClient: ReturnType<typeof useQueryClient>) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: qk.coverageAll() }),
    queryClient.invalidateQueries({ queryKey: qk.adminRunsAll() }),
    invalidateResolved(queryClient),
  ]);
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
