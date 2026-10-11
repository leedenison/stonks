"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListInstrumentsResponse } from "@/gen/admin/instrument/v1/instrument_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useAdminInstruments lists every instrument by name, the page after the
// instrument the token names.
export function useAdminInstruments(
  after: string,
): UseQueryResult<ListInstrumentsResponse> {
  const { adminInstrument } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminInstruments(after),
    queryFn: () => adminInstrument.listInstruments({ pageToken: after }),
  });
}
