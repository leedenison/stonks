"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListInstrumentsResponse } from "@/gen/instrument/v1/instrument_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useInstruments lists the instruments the caller's keys resolved to.
export function useInstruments(): UseQueryResult<ListInstrumentsResponse> {
  const { instrument } = useClients();
  return useAuthedQuery({
    queryKey: qk.instruments(),
    queryFn: () => instrument.listInstruments({}),
  });
}
