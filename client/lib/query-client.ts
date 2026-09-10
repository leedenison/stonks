import { QueryClient } from "@tanstack/react-query";

// Nothing is retried, because the codes the service answers a bad request or
// a missing session with do not change on a second attempt. Nothing refetches
// on focus or reconnect. A cached value paints at once and is revalidated on
// mount, so a view never holds a stale table after data has changed behind it.
export function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 0,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
        staleTime: 0,
      },
    },
  });
}
