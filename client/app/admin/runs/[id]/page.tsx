"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { RotateCcw } from "lucide-react";
import { useParams, useRouter } from "next/navigation";
import { Fragment, type ReactNode, useState } from "react";
import { Button } from "@/app/components/button";
import { Chip } from "@/app/components/chip";
import { EmptyState } from "@/app/components/empty-state";
import {
  IdentifierChip,
  IdentifierChips,
  StatedKeyChips,
} from "@/app/components/identifier-chip";
import { Notice } from "@/app/components/notice";
import { Page } from "@/app/components/page-frame";
import { RejectionGroups } from "@/app/components/rejection-groups";
import { ResolutionChip } from "@/app/components/resolution-chip";
import { Skeleton } from "@/app/components/skeleton";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { Toggle } from "@/app/components/toggle";
import {
  FetchOutcome,
  type Finding,
  FindingKind,
  type GetRunResponse,
  type UserRun,
} from "@/gen/admin/v1/admin_pb";
import { type Run, RunKind } from "@/gen/run/v1/run_pb";
import { AssetClass } from "@/gen/type/v1/type_pb";
import { useAdminRun } from "@/hooks/use-admin-run";
import { useClearBlock } from "@/hooks/use-blocks";
import { useClearFinding } from "@/hooks/use-findings";
import {
  enumLabel,
  filterQuery,
  findingText,
  flattenRuns,
  openFindingsBelow,
  runEnums,
} from "@/lib/admin";
import { formatInstant } from "@/lib/format";
import { ReplayDialog } from "../replay-dialog";

// One run as an administrator reads it: its owner, how it ended, its place
// among the runs above and below it, the findings it recorded and the
// items of its kind. A statement or resolution run carries the action that
// replays its keys.
export default function AdminRunPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { data, isPending, error, refetch } = useAdminRun(id);
  const [replaying, setReplaying] = useState(false);
  const run = data?.run?.run;
  const title = run
    ? `${enumLabel(runEnums.kind, run.kind)} run${run.createdAt ? ` @ ${formatInstant(run.createdAt)}` : ""}`
    : "Run";

  return (
    <Page
      title={title}
      back="/admin/runs"
      width="wide"
      testId="admin-run-page"
      actions={
        run &&
        replayable(run.kind) && (
          <Button
            variant="text"
            data-testid="run-replay"
            onClick={() => setReplaying(true)}
          >
            <RotateCcw aria-hidden className="h-4 w-4" />
            Replay
          </Button>
        )
      }
    >
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
      {replaying && data?.run && (
        <ReplayDialog
          run={data.run}
          onClose={() => setReplaying(false)}
          onStarted={(started) => {
            setReplaying(false);
            router.push(`/admin/runs/${started.id}`);
          }}
        />
      )}
    </Page>
  );
}

// replayable reports whether a run of kind has keys a replay re-resolves.
function replayable(kind: RunKind): boolean {
  return kind === RunKind.STATEMENT || kind === RunKind.RESOLUTION;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {children}
    </section>
  );
}

// Lineage is the run's place in its tree: the runs above it and every run
// below it, without the siblings of any run above. The rows above the run
// start open and the rest start closed.
function Lineage({
  data,
  testId = "admin-run-lineage",
  current = true,
}: {
  data: GetRunResponse;
  testId?: string;
  current?: boolean;
}) {
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
            current={current && id === self.run?.id}
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
    <TableCard testId={testId}>
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

// LineageRow is one run of the lineage at its depth.
function LineageRow({
  run: r,
  depth,
  current = false,
  open,
  toggle,
}: {
  run: UserRun;
  depth: number;
  // current marks the run the page describes. Its row links nowhere and
  // does not change under the pointer.
  current?: boolean;
  // open is unset when the row has no rows below it. A closed row adds the
  // open findings of the rows it hides to its own.
  open?: boolean;
  toggle: (id: string, open: boolean) => void;
}) {
  const id = r.run?.id ?? "";
  const href = `/admin/runs/${id}`;
  const started = r.run?.createdAt ? formatInstant(r.run.createdAt) : "";
  const findings = open === false ? openFindingsBelow(r) : r.openFindings;
  const cells = (
    <>
      <Td className="font-mono tabular-nums">
        <span
          className="flex items-center gap-1"
          style={{ paddingLeft: `${depth * 1.25}rem` }}
        >
          <Toggle
            open={open}
            onToggle={() => toggle(id, !open)}
            testId={`run-toggle-${id}`}
          />
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
        {findings > 0 && (
          <Chip tone="accent" data-testid={`run-open-findings-${id}`}>
            {findings}
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

// Findings lists what the run and every run below it met. A finding that
// reports a block clears with the block, which lifts it.
function Findings({ data }: { data: GetRunResponse }) {
  const clearFinding = useClearFinding();
  const clearBlock = useClearBlock();
  const [openId, setOpenId] = useState<string | null>(null);
  if (data.findings.length === 0) {
    return <EmptyState message="The run recorded no findings." />;
  }
  const pending = clearFinding.isPending || clearBlock.isPending;
  const self = data.run?.run?.id;
  const runs = new Map(
    flattenRuns(data.run ? [data.run] : []).map((r) => [r.run?.id, r.run]),
  );
  const columns = 4;
  return (
    <>
      {clearFinding.isError && (
        <Notice tone="error">The finding could not be cleared.</Notice>
      )}
      {clearBlock.isError && (
        <Notice tone="error">The block could not be cleared.</Notice>
      )}
      <TableCard testId="admin-run-findings">
        <Thead>
          <tr>
            <Th>Run</Th>
            <Th>Kind</Th>
            <Th>Detail</Th>
            <Th>Cleared</Th>
          </tr>
        </Thead>
        <tbody>
          {data.findings.map((f) => {
            const open = openId === f.id;
            const toggle = () => setOpenId(open ? null : f.id);
            return (
              <Fragment key={f.id}>
                <Tr
                  data-testid={`finding-row-${f.id}`}
                  onClick={toggle}
                  className={`cursor-pointer ${open ? "bg-primary-light/15" : ""}`}
                >
                  <Td>
                    <span className="flex items-center gap-1">
                      <Toggle
                        open={open}
                        onToggle={toggle}
                        testId={`finding-toggle-${f.id}`}
                      />
                      <FindingRun
                        run={runs.get(f.runId)}
                        self={f.runId === self}
                      />
                    </span>
                  </Td>
                  <Td>
                    <Chip tone={f.clearedAt ? "muted" : "accent"}>
                      {enumLabel(FindingKind, f.kind)}
                    </Chip>
                  </Td>
                  <Td>{findingText(f)}</Td>
                  <Td onClick={(e) => e.stopPropagation()}>
                    {f.clearedAt ? (
                      <span className="font-mono tabular-nums">
                        {formatInstant(f.clearedAt)}
                      </span>
                    ) : f.blockId ? (
                      <Button
                        variant="secondary"
                        data-testid={`finding-clear-block-${f.id}`}
                        disabled={pending}
                        onClick={() =>
                          f.blockId && clearBlock.mutate(f.blockId)
                        }
                      >
                        Clear block
                      </Button>
                    ) : (
                      <Button
                        variant="secondary"
                        data-testid={`finding-clear-${f.id}`}
                        disabled={pending}
                        onClick={() => clearFinding.mutate(f.id)}
                      >
                        Clear
                      </Button>
                    )}
                  </Td>
                </Tr>
                {open && (
                  <tr data-testid={`finding-detail-${f.id}`}>
                    <td
                      colSpan={columns}
                      className="border-b border-border bg-surface-tint px-4 py-3"
                    >
                      <FindingDetail finding={f} />
                    </td>
                  </tr>
                )}
              </Fragment>
            );
          })}
        </tbody>
      </TableCard>
    </>
  );
}

// FindingDetail is what the key concerned states: each identifier, the
// description, the asset class and the currency, each only where stated.
function FindingDetail({ finding: f }: { finding: Finding }) {
  const k = f.statedKey;
  if (!k) {
    return <p className="text-text-muted">No stated key.</p>;
  }
  const rows: [string, ReactNode][] = [
    [
      "Identifiers",
      k.identifiers.length > 0 && <IdentifierChips ids={k.identifiers} />,
    ],
    ["Description", k.description],
    ["Asset class", k.assetClass !== 0 && enumLabel(AssetClass, k.assetClass)],
    ["Currency", k.currency],
  ];
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1">
      {rows
        .filter(([, v]) => v)
        .map(([k, v]) => (
          <Fragment key={k}>
            <dt className="text-text-muted">{k}</dt>
            <dd>{v}</dd>
          </Fragment>
        ))}
    </dl>
  );
}

// FindingRun names the run that met a finding by its kind and start, linking
// to its page unless it is the run the page describes.
function FindingRun({ run, self }: { run?: Run; self: boolean }) {
  if (!run) return null;
  const label = `${enumLabel(runEnums.kind, run.kind)} @ ${
    run.createdAt ? formatInstant(run.createdAt) : ""
  }`;
  if (self) {
    return <span className="font-mono tabular-nums">{label}</span>;
  }
  return (
    <Link
      href={`/admin/runs/${run.id}`}
      data-testid={`finding-run-${run.id}`}
      className="font-mono text-action tabular-nums underline-offset-4 hover:underline"
    >
      {label}
    </Link>
  );
}

function Items({ data }: { data: GetRunResponse }) {
  if (data.replay) {
    return (
      <Section title="Replay Of">
        <SourceRun id={data.replay.sourceRunId} />
      </Section>
    );
  }
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
                <Td>
                  <StatedKeyChips statedKey={it.statedKey} />
                </Td>
                <Td>
                  <ResolutionChip outcome={it.outcome} />
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
                <Td>
                  <StatedKeyChips statedKey={it.statedKey} />
                </Td>
                <Td>{it.sent && <IdentifierChip id={it.sent} />}</Td>
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

// SourceRun is the tree of the run a replay re-resolved, read as its own
// page would read it, every row of it opening its page.
function SourceRun({ id }: { id: string }) {
  const { data, isPending, error, refetch } = useAdminRun(id);
  if (error) {
    return <Failure error={error} onRetry={() => refetch()} />;
  }
  if (isPending || !data) {
    return <Skeleton lines={2} />;
  }
  return <Lineage data={data} testId="admin-run-source" current={false} />;
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
