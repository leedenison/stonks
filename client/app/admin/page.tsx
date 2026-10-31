"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Pager } from "@/app/components/admin-list";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { useAlertDigest, useAlerts } from "@/hooks/use-alerts";
import { useClearBlock } from "@/hooks/use-blocks";
import { useClearFinding } from "@/hooks/use-findings";
import { type AlertAction, kindSlug, readAlertParams } from "@/lib/alerts";
import { queryHref } from "@/lib/admin";
import { ActionDialog } from "./alert-action";
import { AlertTable } from "./alert-table";
import { AlertTiles } from "./alert-tiles";
import { MockDescriptions } from "./mock-descriptions";

// The admin home lists the alerts that need an administrator. The address
// picks the digest or one kind's paged list.
export default function AdminPage() {
  return (
    <Suspense>
      <Alerts />
    </Suspense>
  );
}

function Alerts() {
  const p = readAlertParams(useSearchParams());
  const digest = useAlertDigest();
  const list = useAlerts(p.kind, p.before);
  const clearBlock = useClearBlock();
  const clearFinding = useClearFinding();
  const [dialog, setDialog] = useState<AlertAction | null>(null);

  const act = (a: AlertAction) => {
    if (a.case === "clear-block") clearBlock.mutate(a.id);
    else if (a.case === "clear-finding") clearFinding.mutate(a.id);
    else setDialog(a);
  };
  const busy = clearBlock.isPending || clearFinding.isPending;

  const groups =
    p.kind === undefined
      ? (digest.data?.groups ?? [])
          .filter((g) => g.count > 0)
          .map((g) => ({ kind: g.kind, count: g.count, alerts: g.top }))
      : [
          {
            kind: p.kind,
            count:
              digest.data?.groups.find((g) => g.kind === p.kind)?.count ?? 0,
            alerts: list.data?.alerts ?? [],
          },
        ];
  const q = p.kind === undefined ? digest : list;
  const slug = p.kind === undefined ? "" : kindSlug(p.kind);

  return (
    <Page title="Alerts" width="wide" testId="alerts-page">
      {digest.data && <AlertTiles digest={digest.data} p={p} />}
      {(clearBlock.isError || clearFinding.isError) && (
        <Notice tone="error">The alert could not be cleared.</Notice>
      )}
      {q.isError && (
        <Notice tone="error" onRetry={() => q.refetch()}>
          The alerts could not be loaded.
        </Notice>
      )}
      {!q.isError && q.data && groups.length === 0 && (
        <EmptyState message="Nothing needs an administrator." />
      )}
      {!q.isError && (q.isPending || groups.length > 0) && (
        <AlertTable
          groups={groups}
          pending={q.isPending}
          busy={busy}
          onAct={act}
          more={p.kind === undefined}
        />
      )}
      {p.kind !== undefined && (
        <Pager
          first={p.before ? queryHref("/admin", { kind: slug }) : undefined}
          next={
            list.data?.nextPageToken
              ? queryHref("/admin", {
                  kind: slug,
                  before: list.data.nextPageToken,
                })
              : undefined
          }
          labels={{ first: "First", next: "Next" }}
        />
      )}
      <MockDescriptions />
      <ActionDialog action={dialog} onClose={() => setDialog(null)} />
    </Page>
  );
}
