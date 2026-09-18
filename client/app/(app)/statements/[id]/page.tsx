"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useParams } from "next/navigation";
import { Chip } from "@/app/components/chip";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { RejectionGroups } from "@/app/components/rejection-groups";
import { Skeleton } from "@/app/components/skeleton";
import { StateChip } from "@/app/components/state-chip";
import { useStatement } from "@/hooks/use-statement";
import { brokerLabel } from "@/lib/broker";
import { formatInstant } from "@/lib/format";
import { prevDay } from "@/lib/marshal/date";
import { outcome } from "@/lib/run";

// One statement: what was uploaded, how its run ended, and every rejected
// row grouped by reason.
export default function StatementPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isPending, error, refetch } = useStatement(id);

  return (
    <Page title="Statement" width="wide" testId="statement-page">
      {error && <Failure error={error} onRetry={() => refetch()} />}
      {!error && isPending && <Skeleton lines={4} />}
      {data?.statement && (
        <>
          <dl
            data-testid="statement-summary"
            className="grid max-w-2xl grid-cols-[max-content_1fr] gap-x-6 gap-y-2 rounded-md border border-border bg-surface px-4 py-3 text-sm"
          >
            <dt className="text-text-muted">Broker</dt>
            <dd>
              <Chip>{brokerLabel(data.statement.broker)}</Chip>
            </dd>
            <dt className="text-text-muted">Period</dt>
            <dd className="font-mono tabular-nums">
              {data.statement.orderFrom} to{" "}
              {prevDay(data.statement.orderBefore)}
            </dd>
            <dt className="text-text-muted">Started</dt>
            <dd className="font-mono tabular-nums">
              {data.statement.run?.createdAt
                ? formatInstant(data.statement.run.createdAt)
                : ""}
            </dd>
            <dt className="text-text-muted">Rows</dt>
            <dd className="font-mono tabular-nums">{data.statement.rows}</dd>
            <dt className="text-text-muted">Rejected</dt>
            <dd
              data-testid="statement-rejected"
              className="font-mono tabular-nums"
            >
              {data.statement.rejected}
            </dd>
            <dt className="text-text-muted">State</dt>
            <dd>
              <StateChip summary={data.statement} />
            </dd>
            {data.statement.run?.error && (
              <>
                <dt className="text-text-muted">Error</dt>
                <dd className="font-mono text-negative">
                  {data.statement.run.error}
                </dd>
              </>
            )}
          </dl>
          <Body
            state={outcome(data.statement)}
            rejected={data.statement.rejected}
          >
            <RejectionGroups items={data.items} />
          </Body>
        </>
      )}
    </Page>
  );
}

function Body({
  state,
  rejected,
  children,
}: {
  state: ReturnType<typeof outcome>;
  rejected: number;
  children: React.ReactNode;
}) {
  switch (state) {
    case "pending":
    case "running":
      return <Notice>The statement is being ingested.</Notice>;
    case "failed":
      return <Notice tone="error">No rows were written.</Notice>;
    case "completed":
      return <Notice>Every row was accepted.</Notice>;
    case "rejections":
      return (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-semibold tracking-tight">
            Rejected rows ({rejected})
          </h2>
          {children}
        </section>
      );
  }
}

function Failure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  if (error instanceof ConnectError && error.code === Code.NotFound) {
    return <Notice>There is no statement at this address.</Notice>;
  }
  return (
    <Notice tone="error" onRetry={onRetry}>
      The statement could not be loaded.
    </Notice>
  );
}
