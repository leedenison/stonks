"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListInstrsResponse } from "@/gen/admin/instr/v1/instr_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import type { InstrParams } from "@/lib/admin";
import { fromParam } from "@/lib/enum";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useAdminInstrs lists the instruments the search and the class keep,
// by name. The page token selects the page.
export function useAdminInstrs(
  p: Pick<InstrParams, "after" | "q" | "class">,
): UseQueryResult<ListInstrsResponse> {
  const { adminInstr } = useClients();
  return useAuthedQuery({
    queryKey: qk.adminInstrs(p.after, p.q, p.class),
    queryFn: () =>
      adminInstr.listInstrs({
        pageToken: p.after,
        query: p.q,
        assetClass: fromParam(AssetClass, p.class),
      }),
  });
}
