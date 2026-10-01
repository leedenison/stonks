"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { Minus, Plus } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { type ReactNode, useState } from "react";
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
  type UserRun,
} from "@/gen/admin/v1/admin_pb";
import { useAdminRun } from "@/hooks/use-admin-run";
import {
  enumLabel,
  filterQuery,
  findingText,
  identifierText,
  keyText,
  runEnums,
} from "@/lib/admin";
import { formatInstant } from "@/lib/format";

// One run as an administrator reads it: its owner, how it ended, its place
// among the runs above and below it, the findings it recorded and the
// items of its kind.
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
          {run.error && (
            <Notice tone="error" testId="admin-run-error">
              {run.error}
            </Notice>
          )}
          <Section title="Started By">
            <Lineage data={data} />
          </Section>
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

// Lineage is the run's place in its tree: the runs above it from the
// top-level run down, the run itself tinted, and every run below it,
// without the siblings of any run above. A row opens its page, and the
// button before the date opens and closes the rows below it; the page
// opens with the rows above the run open and the rest closed.
function Lineage({ data }: { data: GetRunResponse }) {
  const [toggled, setToggled] = useState<Record<string, boolean>>({});
  const self = data.run;
  const rows: ReactNode[] = [];
  const toggle = (id: string, open: boolean) =>
    setToggled({ ...toggled, [id]: open });
  let depth = 0;
  let cut = false;
  for (const a of data.ancestors) {
    const open = toggled[a.run?.id ?? ""] ?? true;
    rows.push(
      <LineageRow
        key={a.run?.id}
        run={a}
        depth={depth}
        open={open}
        toggle={toggle}
      />,
    );
    if (!open) {
      cut = true;
      break;
    }
    depth++;
  }
  if (self && !cut) {
    const below = (runs: UserRun[], depth: number) => {
      for (const r of runs) {
        const id = r.run?.id ?? "";
        const open = toggled[id] ?? false;
        rows.push(
          <LineageRow
            key={id}
            run={r}
            depth={depth}
            current={id === self.run?.id}
            open={r.children.length > 0 ? open : undefined}
            toggle={toggle}
          />,
        );
        if (open) {
          below(r.children, depth + 1);
        }
      }
    };
    below([self], depth);
  }
  return (
    <TableCard testId="admin-run-lineage">
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
      <tbody>{rows}</tbody>
    </TableCard>
  );
}

// LineageRow is one run of the lineage at its depth. open is set where the
// row has rows below it. current marks the run the page describes, which
// neither opens its own page nor changes under the pointer.
function LineageRow({
  run: r,
  depth,
  current = false,
  open,
  toggle,
}: {
  run: UserRun;
  depth: number;
  current?: boolean;
  open?: boolean;
  toggle: (id: string, open: boolean) => void;
}) {
  const id = r.run?.id ?? "";
  const href = `/admin/runs/${id}`;
  const started = r.run?.createdAt ? formatInstant(r.run.createdAt) : "";
  const Toggle = open ? Minus : Plus;
  const cells = (
    <>
      <Td className="font-mono tabular-nums">
        <span
          className="flex items-center gap-1"
          style={{ paddingLeft: `${depth * 1.25}rem` }}
        >
          {open === undefined ? (
            <span className="size-4" aria-hidden="true" />
          ) : (
            <button
              type="button"
              aria-expanded={open}
              aria-label={open ? "Close" : "Open"}
              data-testid={`run-toggle-${id}`}
              onClick={(e) => {
                e.stopPropagation();
                toggle(id, !open);
              }}
              className="rounded border border-border text-text-muted hover:text-text-primary"
            >
              <Toggle className="size-4" aria-hidden="true" />
            </button>
          )}
          {current ? (
            started
          ) : (
            <Link href={href} className="underline-offset-4 hover:underline">
              {started}
            </Link>
          )}
        </span>
      </Td>
      <Td>
        <Chip>{enumLabel(runEnums.kind, r.run?.kind ?? 0)}</Chip>
      </Td>
      <Td>{enumLabel(runEnums.trigger, r.run?.trigger ?? 0)}</Td>
      <Td>
        <Link
          href={filterQuery({
            kind: "",
            trigger: "",
            state: "",
            before: "",
            user: r.userId,
          })}
          onClick={(e) => e.stopPropagation()}
          className="text-action underline-offset-4 hover:underline"
        >
          {r.userEmail}
        </Link>
      </Td>
      <Td>
        <RunChip run={r.run} />
      </Td>
      <Td className="font-mono tabular-nums">
        {r.openFindings > 0 && (
          <Chip tone="accent" data-testid={`run-open-findings-${id}`}>
            {r.openFindings}
          </Chip>
        )}
      </Td>
    </>
  );
  if (current) {
    return (
      <tr
        data-testid={`run-row-${id}`}
        aria-current="page"
        className="bg-accent-soft/50"
      >
        {cells}
      </tr>
    );
  }
  return (
    <Tr href={href} data-testid={`run-row-${id}`}>
      {cells}
    </Tr>
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
          <Th>Detail</Th>
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
            <Td>{findingText(f)}</Td>
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
