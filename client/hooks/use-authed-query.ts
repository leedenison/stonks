"use client";

import {
  type UseQueryOptions,
  type UseQueryResult,
  useQuery,
} from "@tanstack/react-query";
import { useAuth } from "@/contexts/auth-context";

// useAuthedQuery is useQuery for a query that needs a session. It waits while
// the session is being restored and does not run without one, so a request
// never fails as unauthenticated and expires a session that was only late.
// The caller's own enabled is combined with the gate rather than replaced.
export function useAuthedQuery<
  TQueryFnData = unknown,
  TError = Error,
  TData = TQueryFnData,
>(
  options: UseQueryOptions<TQueryFnData, TError, TData>,
): UseQueryResult<TData, TError> {
  const { state } = useAuth();
  return useQuery({
    ...options,
    enabled: state.status === "authenticated" && (options.enabled ?? true),
  });
}
