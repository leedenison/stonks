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
import { BlockScope, FetchKind } from "@/gen/admin/v1/admin_pb";
import { useBlocks, useClearBlock } from "@/hooks/use-blocks";
import { enumLabel, identifierText, readList } from "@/lib/admin";
import { formatInstant } from "@/lib/format";

const path = "/admin/blocks";

// The calls a datasource refused and that stay suppressed until cleared,
// newest first. Clearing a block clears the finding reporting it.
export default function BlocksPage() {
  return (
    <Suspense>
      <Blocks />
    </Suspense>
  );
}

function Blocks() {
  const p = readList(useSearchParams());
  const { data, isPending, isError, refetch } = useBlocks(p);
  const clear = useClearBlock();
  const blocks = data?.blocks ?? [];

  return (
    <Page title="Blocks" width="wide" testId="admin-blocks-page">
      <ClearedToggle path={path} p={p} />
      {clear.isError && (
        <Notice tone="error">The block could not be cleared.</Notice>
      )}
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The blocks could not be loaded.
        </Notice>
      )}
      {!isError && data && blocks.length === 0 && (
        <EmptyState message="No blocks." />
      )}
      {!isError && (isPending || blocks.length > 0) && (
        <TableCard testId="admin-blocks-table">
          <Thead>
            <tr>
              <Th>Created</Th>
              <Th>Datasource</Th>
              <Th>Blocked</Th>
              <Th>Reason</Th>
              <Th>Run</Th>
              <Th>Cleared</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={6} />
          ) : (
            <tbody>
              {blocks.map((b) => (
                <Tr key={b.id} data-testid={`block-row-${b.id}`}>
                  <Td className="font-mono tabular-nums">
                    {b.createdAt ? formatInstant(b.createdAt) : ""}
                  </Td>
                  <Td>
                    {b.datasource} <Chip>{enumLabel(FetchKind, b.kind)}</Chip>
                  </Td>
                  <Td className="font-mono">
                    {b.scope === BlockScope.IDENTIFIER && b.sent
                      ? identifierText(b.sent)
                      : "every call"}
                  </Td>
                  <Td>{b.reason}</Td>
                  <Td>
                    <Link
                      href={`/admin/runs/${b.runId}`}
                      className="font-mono text-action underline-offset-4 hover:underline"
                    >
                      {b.runId}
                    </Link>
                  </Td>
                  <Td>
                    {b.clearedAt ? (
                      <span className="font-mono tabular-nums">
                        {formatInstant(b.clearedAt)}
                      </span>
                    ) : (
                      <Button
                        variant="secondary"
                        data-testid={`block-clear-${b.id}`}
                        disabled={clear.isPending}
                        onClick={() => clear.mutate(b.id)}
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
