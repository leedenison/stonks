"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type {
  AlertKind,
  GetAlertDigestResponse,
  ListAlertsResponse,
} from "@/gen/admin/alert/v1/alert_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// digestInterval is how often the digest is read while a page shows it, in
// milliseconds. It also bounds how long the nav badge lags a run that a user
// started, because no administrator action refreshes the alerts for that
// run.
const digestInterval = 30_000;

// useAlertDigest reads the count and the top alerts of each kind. The digest
// derives the ticker kinds for every user on each read, so a page that
// mounts it again within the interval reads the copy it has.
export function useAlertDigest(): UseQueryResult<GetAlertDigestResponse> {
  const { alert } = useClients();
  return useAuthedQuery({
    queryKey: qk.alertDigest(),
    queryFn: () => alert.getAlertDigest({}),
    staleTime: digestInterval,
    refetchInterval: digestInterval,
  });
}

// useAlerts lists one kind's alerts in rank order. An empty token is the first
// page. It does not run without a kind.
export function useAlerts(
  kind: AlertKind | undefined,
  token: string,
): UseQueryResult<ListAlertsResponse> {
  const { alert } = useClients();
  return useAuthedQuery({
    queryKey: qk.alerts(kind ?? 0, token),
    queryFn: () => alert.listAlerts({ kind, pageToken: token }),
    enabled: kind !== undefined,
  });
}
