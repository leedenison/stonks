"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Fragment, Suspense, useState } from "react";
import { LinkButton } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { Toggle } from "@/app/components/toggle";
import type { UserRun } from "@/gen/admin/v1/admin_pb";
import { useAdminRuns } from "@/hooks/use-admin-runs";
import {
  enumLabel,
  enumParam,
  enumValues,
  filterQuery,
  openFindingsBelow,
  readFilters,
  type RunFilters,
  runEnums,
} from "@/lib/admin";
import { formatInstant } from "@/lib/format";

// Every user's runs, newest first, filtered by what the address names, each
// top-level run a row that opens onto the runs below it. The filters live in
// the address so a listing can be linked and paged.
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
  // toggled holds the rows an administrator opened or closed; a row not in
  // it is open when it is on the path to a match rather than a match.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});
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
              <Th>Findings</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={6} />
          ) : (
            <tbody>
              <RunRows
                runs={runs}
                depth={0}
                filters={f}
                toggled={toggled}
                toggle={(id, open) => setToggled({ ...toggled, [id]: open })}
              />
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

// RunRows renders runs and, under each open one, its children indented a
// level deeper. A row opens its page; the button before the date opens and
// closes its children. A closed row with children shows the findings of its
// whole subtree.
function RunRows({
  runs,
  depth,
  filters,
  toggled,
  toggle,
}: {
  runs: UserRun[];
  depth: number;
  filters: RunFilters;
  toggled: Record<string, boolean>;
  toggle: (id: string, open: boolean) => void;
}) {
  return (
    <>
      {runs.map((r) => {
        const { run, userId, userEmail, openFindings, matched, children } = r;
        const id = run?.id ?? "";
        const href = `/admin/runs/${id}`;
        const open = toggled[id] ?? !matched;
        const findings =
          open || children.length === 0 ? openFindings : openFindingsBelow(r);
        return (
          <Fragment key={id}>
            <Tr
              href={href}
              data-testid={`run-row-${id}`}
              className={matched ? "" : "text-text-muted"}
            >
              <Td className="font-mono tabular-nums">
                <span
                  className="flex items-center gap-1"
                  style={{ paddingLeft: `${depth * 1.25}rem` }}
                >
                  <Toggle
                    open={children.length > 0 ? open : undefined}
                    onToggle={() => toggle(id, !open)}
                    testId={`run-toggle-${id}`}
                  />
                  <Link
                    href={href}
                    className="underline-offset-4 hover:underline"
                  >
                    {run?.createdAt ? formatInstant(run.createdAt) : ""}
                  </Link>
                </span>
              </Td>
              <Td>
                <Chip>{enumLabel(runEnums.kind, run?.kind ?? 0)}</Chip>
              </Td>
              <Td>{enumLabel(runEnums.trigger, run?.trigger ?? 0)}</Td>
              <Td>
                <Link
                  href={filterQuery({ ...filters, before: "", user: userId })}
                  onClick={(e) => e.stopPropagation()}
                  className="text-action underline-offset-4 hover:underline"
                >
                  {userEmail}
                </Link>
              </Td>
              <Td>
                <RunChip run={run} />
              </Td>
              <Td className="font-mono tabular-nums">
                {findings > 0 && (
                  <Chip tone="accent" data-testid={`run-open-findings-${id}`}>
                    {findings}
                  </Chip>
                )}
              </Td>
            </Tr>
            {open && children.length > 0 && (
              <RunRows
                runs={children}
                depth={depth + 1}
                filters={filters}
                toggled={toggled}
                toggle={toggle}
              />
            )}
          </Fragment>
        );
      })}
    </>
  );
}
