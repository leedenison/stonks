"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { ClearedToggle, Pager } from "@/app/components/admin-list";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { FindingKind } from "@/gen/admin/v1/admin_pb";
import { useClearFinding, useFindings } from "@/hooks/use-findings";
import { enumLabel, readList } from "@/lib/admin";
import { formatInstant } from "@/lib/format";

const path = "/admin/findings";

// What runs met that an administrator may need to see, newest first. A
// finding reporting a block is cleared with the block, so it leads to the
// blocks page rather than offering a clear of its own.
export default function FindingsPage() {
  return (
    <Suspense>
      <Findings />
    </Suspense>
  );
}

function Findings() {
  const p = readList(useSearchParams());
  const { data, isPending, isError, refetch } = useFindings(p);
  const clear = useClearFinding();
  const findings = data?.findings ?? [];

  return (
    <Page title="Findings" width="wide" testId="admin-findings-page">
      <ClearedToggle path={path} p={p} />
      {clear.isError && (
        <Notice tone="error">The finding could not be cleared.</Notice>
      )}
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The findings could not be loaded.
        </Notice>
      )}
      {!isError && data && findings.length === 0 && (
        <EmptyState message="No findings. What a run meets that needs an administrator is listed here." />
      )}
      {!isError && (isPending || findings.length > 0) && (
        <TableCard testId="admin-findings-table">
          <Thead>
            <tr>
              <Th>Recorded</Th>
              <Th>Kind</Th>
              <Th>Run</Th>
              <Th>Cleared</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={4} />
          ) : (
            <tbody>
              {findings.map((f) => (
                <Tr key={f.id} data-testid={`finding-row-${f.id}`}>
                  <Td className="font-mono tabular-nums">
                    {f.createdAt ? formatInstant(f.createdAt) : ""}
                  </Td>
                  <Td>
                    <Chip tone={f.clearedAt ? "muted" : "accent"}>
                      {enumLabel(FindingKind, f.kind)}
                    </Chip>
                  </Td>
                  <Td>
                    <Link
                      href={`/admin/runs/${f.runId}`}
                      className="font-mono text-action underline-offset-4 hover:underline"
                    >
                      {f.runId}
                    </Link>
                  </Td>
                  <Td>
                    {f.clearedAt ? (
                      <span className="font-mono tabular-nums">
                        {formatInstant(f.clearedAt)}
                      </span>
                    ) : f.blockId ? (
                      <Link
                        href="/admin/blocks"
                        data-testid={`finding-block-${f.id}`}
                        className="text-action underline-offset-4 hover:underline"
                      >
                        Clear the block
                      </Link>
                    ) : (
                      <Button
                        variant="secondary"
                        data-testid={`finding-clear-${f.id}`}
                        disabled={clear.isPending}
                        onClick={() => clear.mutate(f.id)}
                      >
                        Clear
                      </Button>
                    )}
                  </Td>
                </Tr>
              ))}
            </tbody>
          )}
        </TableCard>
      )}
      <Pager path={path} p={p} next={data?.nextPageToken} />
    </Page>
  );
}
