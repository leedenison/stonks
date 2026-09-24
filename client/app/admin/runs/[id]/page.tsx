"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useParams } from "next/navigation";
import type { ReactNode } from "react";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { RejectionGroups } from "@/app/components/rejection-groups";
import { Skeleton } from "@/app/components/skeleton";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import {
  FetchOutcome,
  FindingKind,
  type GetRunResponse,
  ResolutionOutcome,
} from "@/gen/admin/v1/admin_pb";
import { useAdminRun } from "@/hooks/use-admin-run";
import {
  enumLabel,
  filterQuery,
  identifierText,
  keyText,
  runEnums,
} from "@/lib/admin";
import { formatInstant } from "@/lib/format";

// One run as an administrator reads it: who it belongs to, how it ended,
// the runs it started, the findings it recorded and the items of its kind.
export default function AdminRunPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isPending, error, refetch } = useAdminRun(id);
  const run = data?.run?.run;
  const title = run
    ? `${enumLabel(runEnums.kind, run.kind)} run${run.createdAt ? ` @ ${formatInstant(run.createdAt)}` : ""}`
    : "Run";

  return (
    <Page title={title} back="/admin/runs" width="wide" testId="admin-run-page">
      {error && <Failure error={error} onRetry={() => refetch()} />}
      {!error && isPending && <Skeleton lines={4} />}
      {data && run && (
        <>
          <dl
            data-testid="admin-run-summary"
            className="grid max-w-2xl grid-cols-[max-content_1fr] gap-x-6 gap-y-2 rounded-md border border-border bg-surface px-4 py-3 text-sm"
          >
            <dt className="text-text-muted">User</dt>
            <dd>
              <Link
                href={filterQuery({
                  kind: "",
                  trigger: "",
                  state: "",
                  before: "",
                  user: data.run?.userId ?? "",
                })}
                className="text-action underline-offset-4 hover:underline"
              >
                {data.run?.userEmail}
              </Link>
            </dd>
            <dt className="text-text-muted">Trigger</dt>
            <dd>{enumLabel(runEnums.trigger, run.trigger)}</dd>
            {run.parentId && (
              <>
                <dt className="text-text-muted">Parent</dt>
                <dd>
                  <Link
                    href={`/admin/runs/${run.parentId}`}
                    data-testid="admin-run-parent"
                    className="font-mono text-action underline-offset-4 hover:underline"
                  >
                    {run.parentId}
                  </Link>
                </dd>
              </>
            )}
            <dt className="text-text-muted">State</dt>
            <dd>
              <RunChip run={run} />
            </dd>
            {run.finishedAt && (
              <>
                <dt className="text-text-muted">Finished</dt>
                <dd className="font-mono tabular-nums">
                  {formatInstant(run.finishedAt)}
                </dd>
              </>
            )}
            {run.error && (
              <>
                <dt className="text-text-muted">Error</dt>
                <dd className="font-mono text-negative">{run.error}</dd>
              </>
            )}
          </dl>
          {data.children.length > 0 && (
            <Section title="Started">
              <Children data={data} />
            </Section>
          )}
          <Section title="Findings">
            <Findings data={data} />
          </Section>
          <Items data={data} />
        </>
      )}
    </Page>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {children}
    </section>
  );
}

function Children({ data }: { data: GetRunResponse }) {
  return (
    <TableCard testId="admin-run-children">
      <Thead>
        <tr>
          <Th>Started</Th>
          <Th>Kind</Th>
          <Th>State</Th>
        </tr>
      </Thead>
      <tbody>
        {data.children.map((c) => {
          const href = `/admin/runs/${c.id}`;
          return (
            <Tr key={c.id} href={href} data-testid={`run-row-${c.id}`}>
              <Td className="font-mono tabular-nums">
                <Link
                  href={href}
                  className="underline-offset-4 hover:underline"
                >
                  {c.createdAt ? formatInstant(c.createdAt) : ""}
                </Link>
              </Td>
              <Td>
                <Chip>{enumLabel(runEnums.kind, c.kind)}</Chip>
              </Td>
              <Td>
                <RunChip run={c} />
              </Td>
            </Tr>
          );
        })}
      </tbody>
    </TableCard>
  );
}

function Findings({ data }: { data: GetRunResponse }) {
  if (data.findings.length === 0) {
    return <EmptyState message="The run recorded no findings." />;
  }
  return (
    <TableCard testId="admin-run-findings">
      <Thead>
        <tr>
          <Th>Recorded</Th>
          <Th>Kind</Th>
          <Th>Cleared</Th>
        </tr>
      </Thead>
      <tbody>
        {data.findings.map((f) => (
          <Tr key={f.id} data-testid={`finding-row-${f.id}`}>
            <Td className="font-mono tabular-nums">
              {f.createdAt ? formatInstant(f.createdAt) : ""}
            </Td>
            <Td>
              <Chip tone={f.clearedAt ? "muted" : "accent"}>
                {enumLabel(FindingKind, f.kind)}
              </Chip>
            </Td>
            <Td className="font-mono tabular-nums">
              {f.clearedAt ? formatInstant(f.clearedAt) : ""}
            </Td>
          </Tr>
        ))}
      </tbody>
    </TableCard>
  );
}

function Items({ data }: { data: GetRunResponse }) {
  if (data.statementItems.length > 0) {
    return (
      <Section title={`Rejected rows (${data.statementItems.length})`}>
        <RejectionGroups items={data.statementItems} />
      </Section>
    );
  }
  if (data.resolutionItems.length > 0) {
    return (
      <Section title="Keys">
        <TableCard testId="admin-run-items">
          <Thead>
            <tr>
              <Th>Stated key</Th>
              <Th>Outcome</Th>
              <Th>Reason</Th>
            </tr>
          </Thead>
          <tbody>
            {data.resolutionItems.map((it) => (
              <Tr
                key={it.statedKeyId}
                data-testid={`item-row-${it.statedKeyId}`}
              >
                <Td className="font-mono">{keyText(it.statedKey)}</Td>
                <Td>
                  <Chip>{enumLabel(ResolutionOutcome, it.outcome)}</Chip>
                </Td>
                <Td>{it.reason}</Td>
              </Tr>
            ))}
          </tbody>
        </TableCard>
      </Section>
    );
  }
  if (data.fetchItems.length > 0) {
    return (
      <Section title="Keys">
        <TableCard testId="admin-run-items">
          <Thead>
            <tr>
              <Th>Stated key</Th>
              <Th>Sent</Th>
              <Th>Outcome</Th>
              <Th numeric>Attempts</Th>
              <Th>Reason</Th>
            </tr>
          </Thead>
          <tbody>
            {data.fetchItems.map((it) => (
              <Tr
                key={it.statedKeyId}
                data-testid={`item-row-${it.statedKeyId}`}
              >
                <Td className="font-mono">{keyText(it.statedKey)}</Td>
                <Td className="font-mono">
                  {it.sent ? identifierText(it.sent) : ""}
                </Td>
                <Td>
                  <Chip>{enumLabel(FetchOutcome, it.outcome)}</Chip>
                </Td>
                <Td numeric>{it.attempts}</Td>
                <Td>{it.reason}</Td>
              </Tr>
            ))}
          </tbody>
        </TableCard>
      </Section>
    );
  }
  return null;
}

function Failure({ error, onRetry }: { error: Error; onRetry: () => void }) {
  if (error instanceof ConnectError && error.code === Code.NotFound) {
    return <Notice>There is no run at this address.</Notice>;
  }
  return (
    <Notice tone="error" onRetry={onRetry}>
      The run could not be loaded.
    </Notice>
  );
}
