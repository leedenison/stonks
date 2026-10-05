"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useAuth } from "@/contexts/auth-context";
import { useClients } from "@/contexts/clients-context";
import { qk } from "@/lib/query-keys";
import { pollInterval } from "@/lib/run";

// useAdminRunItems reads the items a run wrote, a page at a time, polling
// while live. A run is live while it or a run below it is still writing. As
// useAuthedQuery does, it waits for a session.
export function useAdminRunItems(id: string, live: boolean) {
  const { admin } = useClients();
  const { state } = useAuth();
  return useInfiniteQuery({
    queryKey: qk.adminRunItems(id),
    queryFn: ({ pageParam }) =>
      admin.listRunItems({ runId: id, pageToken: pageParam }),
    initialPageParam: "",
    getNextPageParam: (last) => last.nextPageToken || undefined,
    enabled: state.status === "authenticated",
    refetchInterval: live ? pollInterval : false,
  });
}
