"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Fragment, Suspense, useState } from "react";
import { Pager } from "@/app/components/admin-list";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { Select } from "@/app/components/input";
import type { UserRun } from "@/gen/admin/v1/admin_pb";
import { useAdminRuns } from "@/hooks/use-admin-runs";
import {
  filterQuery,
  readFilters,
  type RunFilters,
  runEnums,
} from "@/lib/admin";
import { enumLabel, enumParam, enumValues } from "@/lib/enum";
import { RunRow, RunTreeTable } from "./run-tree";

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
            <Select
              data-testid={`runs-filter-${k}`}
              value={f[k]}
              onChange={(e) => go({ [k]: e.target.value })}
            >
              <option value="">any</option>
              {enumValues(runEnums[k]).map((v) => (
                <option key={v} value={enumParam(runEnums[k], v)}>
                  {enumLabel(runEnums[k], v)}
                </option>
              ))}
            </Select>
          </label>
        ))}
        {f.user && (
          <Chip tone="primary" data-testid="runs-filter-user">
            {runs[0]?.userEmail ?? f.user}
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
        <RunTreeTable testId="admin-runs-table" pending={isPending}>
          <RunRows
            runs={runs}
            depth={0}
            filters={f}
            toggled={toggled}
            toggle={(id, open) => setToggled((t) => ({ ...t, [id]: open }))}
          />
        </RunTreeTable>
      )}
      <Pager
        newest={f.before ? filterQuery({ ...f, before: "" }) : undefined}
        older={
          data?.nextPageToken
            ? filterQuery({ ...f, before: data.nextPageToken })
            : undefined
        }
      />
    </Page>
  );
}

// RunRows renders runs and, under each open one, its children indented a
// level deeper.
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
        const id = r.run?.id ?? "";
        const open = toggled[id] ?? !r.matched;
        return (
          <Fragment key={id}>
            <RunRow
              run={r}
              depth={depth}
              open={r.children.length > 0 ? open : undefined}
              onToggle={() => toggle(id, !open)}
              muted={!r.matched}
              filters={filters}
            />
            {open && r.children.length > 0 && (
              <RunRows
                runs={r.children}
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
