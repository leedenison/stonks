"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import {
  type UseQueryResult,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  ListDatasourcesResponse,
  UpdateDatasourceRequestSchema,
} from "@/gen/admin/v1/admin_pb";
import { reordered } from "@/lib/datasources";
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

// useUpdateDatasource sets a datasource's state, endpoint and credential,
// and refreshes the listing.
export function useUpdateDatasource() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: MessageInitShape<typeof UpdateDatasourceRequestSchema>) =>
      admin.updateDatasource(req),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.datasources() }),
  });
}

// useReorderDatasources writes the order into the listing before the service
// confirms it, so a drag lands where it was dropped, and puts the previous
// order back if the service refuses.
export function useReorderDatasources() {
  const { admin } = useClients();
  const queryClient = useQueryClient();
  const key = qk.datasources();
  return useMutation({
    mutationFn: (names: string[]) => admin.reorderDatasources({ names }),
    onMutate: async (names) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<ListDatasourcesResponse>(key);
      if (previous) queryClient.setQueryData(key, reordered(previous, names));
      return { previous };
    },
    onError: (_error, _names, context) => {
      if (context?.previous) queryClient.setQueryData(key, context.previous);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}
