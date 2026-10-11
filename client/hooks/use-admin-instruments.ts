"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListInstrumentsResponse } from "@/gen/admin/instrument/v1/instrument_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import type { InstrumentParams } from "@/lib/admin";
import { fromParam } from "@/lib/enum";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useAdminInstruments lists the instruments the search and the class keep,
// by name, the page after the instrument the token names.
export function useAdminInstruments(
  p: InstrumentParams,
): UseQueryResult<ListInstrumentsResponse> {
  const { adminInstrument } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminInstruments(p.after, p.q, p.class),
    queryFn: () =>
      adminInstrument.listInstruments({
        pageToken: p.after,
        query: p.q,
        assetClass: fromParam(AssetClass, p.class),
      }),
  });
}
