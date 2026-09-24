"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { LinkButton } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { useAdminRuns } from "@/hooks/use-admin-runs";
import {
  enumLabel,
  enumParam,
  enumValues,
  filterQuery,
  readFilters,
  type RunFilters,
  runEnums,
} from "@/lib/admin";
import { formatInstant } from "@/lib/format";

// Every user's runs, newest first, filtered by what the address names. The
// filters live in the address so a listing can be linked to and paged.
export default function RunsPage() {
  return (
    <Suspense>
      <Runs />
    </Suspense>
  );
}

function Runs() {
  const f = readFilters(useSearchParams());
  const router = useRouter();
  const { data, isPending, isError, refetch } = useAdminRuns(f);
  const runs = data?.runs ?? [];
  const go = (next: Partial<RunFilters>) =>
    router.replace(filterQuery({ ...f, before: "", ...next }));

  return (
    <Page title="Runs" width="wide" testId="admin-runs-page">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        {(["kind", "trigger", "state"] as const).map((k) => (
          <label key={k} className="flex items-center gap-2">
            <span className="text-text-muted capitalize">{k}</span>
            <select
              data-testid={`runs-filter-${k}`}
              value={f[k]}
              onChange={(e) => go({ [k]: e.target.value })}
              className="rounded-md border border-border bg-surface px-2 py-1"
            >
              <option value="">any</option>
              {enumValues(runEnums[k]).map((v) => (
                <option key={v} value={enumParam(runEnums[k], v)}>
                  {enumLabel(runEnums[k], v)}
                </option>
              ))}
            </select>
          </label>
        ))}
        {f.user && (
          <Chip tone="primary" data-testid="runs-filter-user">
            {runs[0]?.userEmail ?? "One user"}
            <button
              type="button"
              aria-label="Show every user"
              onClick={() => go({ user: "" })}
              className="ml-1"
            >
              ×
            </button>
          </Chip>
        )}
      </div>
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The runs could not be loaded.
        </Notice>
      )}
      {!isError && data && runs.length === 0 && (
        <EmptyState message="No runs match." />
      )}
      {!isError && (isPending || runs.length > 0) && (
        <TableCard testId="admin-runs-table">
          <Thead>
            <tr>
              <Th>Started</Th>
              <Th>Kind</Th>
              <Th>Trigger</Th>
              <Th>User</Th>
              <Th>State</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={5} />
          ) : (
            <tbody>
              {runs.map(({ run, userId, userEmail }) => {
                const id = run?.id ?? "";
                const href = `/admin/runs/${id}`;
                return (
                  <Tr key={id} href={href} data-testid={`run-row-${id}`}>
                    <Td className="font-mono tabular-nums">
                      <Link
                        href={href}
                        className="underline-offset-4 hover:underline"
                      >
                        {run?.createdAt ? formatInstant(run.createdAt) : ""}
                      </Link>
                    </Td>
                    <Td>
                      <Chip>{enumLabel(runEnums.kind, run?.kind ?? 0)}</Chip>
                    </Td>
                    <Td>{enumLabel(runEnums.trigger, run?.trigger ?? 0)}</Td>
                    <Td>
                      <Link
                        href={filterQuery({ ...f, before: "", user: userId })}
                        onClick={(e) => e.stopPropagation()}
                        className="text-action underline-offset-4 hover:underline"
                      >
                        {userEmail}
                      </Link>
                    </Td>
                    <Td>
                      <RunChip run={run} />
                    </Td>
                  </Tr>
                );
              })}
            </tbody>
          )}
        </TableCard>
      )}
      {(f.before || data?.nextPageToken) && (
        <div className="flex gap-2">
          {f.before && (
            <LinkButton
              variant="secondary"
              href={filterQuery({ ...f, before: "" })}
            >
              Newest
            </LinkButton>
          )}
          {data?.nextPageToken && (
            <LinkButton
              variant="secondary"
              data-testid="runs-older"
              href={filterQuery({ ...f, before: data.nextPageToken })}
            >
              Older
            </LinkButton>
          )}
        </div>
      )}
    </Page>
  );
}
