"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { ListDatasourcesResponse } from "@/gen/admin/v1/admin_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useDatasources lists the registered datasources in precedence order.
export function useDatasources(): UseQueryResult<ListDatasourcesResponse> {
  const { admin } = useClients();
  return useAuthedQuery({
    queryKey: qk.datasources(),
    queryFn: () => admin.listDatasources({}),
  });
}
