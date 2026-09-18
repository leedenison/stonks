"use client";

import Link from "next/link";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { StateChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { UploadAction } from "@/app/components/upload-action";
import { useStatements } from "@/hooks/use-statements";
import { brokerLabel } from "@/lib/broker";
import { formatInstant } from "@/lib/format";
import { prevDay } from "@/lib/marshal/date";

// The user's statements, newest first, each leading to its own page.
export default function StatementsPage() {
  const { data, isPending, isError, refetch } = useStatements();
  const statements = data?.statements ?? [];

  return (
    <Page
      title="Statements"
      width="wide"
      testId="statements-page"
      actions={<UploadAction />}
    >
      {isError && (
        <Notice tone="error" onRetry={() => refetch()}>
          The statements could not be loaded.
        </Notice>
      )}
      {!isError && data && statements.length === 0 && (
        <EmptyState message="No statements yet. Each upload is listed here with its outcome." />
      )}
      {!isError && (isPending || statements.length > 0) && (
        <TableCard testId="statements-table">
          <Thead>
            <tr>
              <Th>Started</Th>
              <Th>Broker</Th>
              <Th>Period</Th>
              <Th numeric>Rows</Th>
              <Th numeric>Rejected</Th>
              <Th>State</Th>
            </tr>
          </Thead>
          {isPending ? (
            <SkeletonRows columns={6} />
          ) : (
            <tbody>
              {statements.map((s) => {
                const id = s.run?.id ?? "";
                const href = `/statements/${id}`;
                return (
                  <Tr key={id} href={href} data-testid={`statement-row-${id}`}>
                    <Td className="font-mono tabular-nums">
                      <Link
                        href={href}
                        className="underline-offset-4 hover:underline"
                      >
                        {s.run?.createdAt ? formatInstant(s.run.createdAt) : ""}
                      </Link>
                    </Td>
                    <Td>
                      <Chip>{brokerLabel(s.broker)}</Chip>
                    </Td>
                    <Td className="font-mono tabular-nums">
                      {s.orderFrom} to {prevDay(s.orderBefore)}
                    </Td>
                    <Td numeric>{s.rows}</Td>
                    <Td numeric data-testid="statement-rejected">
                      {s.rejected}
                    </Td>
                    <Td>
                      <StateChip summary={s} />
                    </Td>
                  </Tr>
                );
              })}
            </tbody>
          )}
        </TableCard>
      )}
    </Page>
  );
}
