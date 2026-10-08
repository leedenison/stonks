"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useParams } from "next/navigation";
import { Chip } from "@/app/components/chip";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { RejectionGroups } from "@/app/components/rejection-groups";
import { ResolutionKeys } from "@/app/components/resolution-keys";
import { Section } from "@/app/components/section";
import { Skeleton } from "@/app/components/skeleton";
import { StateChip } from "@/app/components/state-chip";
import type { ResolutionItem } from "@/gen/type/v1/type_pb";
import { useStatement } from "@/hooks/use-statement";
import { brokerLabel } from "@/lib/broker";
import { formatInstant } from "@/lib/format";
import { prevDay } from "@/lib/marshal/date";
import { isTerminal, outcome } from "@/lib/run";

// One statement, with its run, its rejected rows and the resolution of every
// key it stated. The title names the upload by its broker and the moment it
// started.
export default function StatementPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isPending, error, refetch } = useStatement(id);
  const s = data?.statement;
  const title = s?.run?.createdAt
    ? `${brokerLabel(s.broker)} upload @ ${formatInstant(s.run.createdAt)}`
    : "Statement";

  return (
    <Page title={title} back="/statements" width="wide" testId="statement-page">
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
            <dt className="text-text-muted">From</dt>
            <dd className="font-mono tabular-nums">
              {data.statement.orderFrom}
            </dd>
            <dt className="text-text-muted">To</dt>
            <dd className="font-mono tabular-nums">
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
          {data.keys.length > 0 && (
            <Keys
              keys={data.keys}
              live={!isTerminal(data.statement.run?.state)}
            />
          )}
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
      return <Notice tone="positive">All rows are valid.</Notice>;
    case "rejections":
      return (
        <Section title={`Rejected rows (${rejected})`}>{children}</Section>
      );
  }
}

// Keys lists what the statement stated, one row per key, with the outcome
// of the key's latest resolution.
function Keys({ keys, live }: { keys: ResolutionItem[]; live: boolean }) {
  return (
    <Section title="Keys">
      <ResolutionKeys
        keys={keys}
        live={live}
        testId="statement-keys"
        rowTestId={(id) => `key-row-${id}`}
      />
    </Section>
  );
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
