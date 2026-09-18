"use client";

import { ArrowRight, ChevronRight } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useActivity } from "@/contexts/activity-context";
import type { StatementSummary } from "@/gen/statement/v1/statement_pb";
import { useStatement } from "@/hooks/use-statement";
import { brokerLabel } from "@/lib/broker";
import { formatInstant } from "@/lib/format";
import { prevDay } from "@/lib/marshal/date";
import { outcome } from "@/lib/run";
import { LinkButton } from "./button";
import { Notice } from "./notice";
import { RejectionGroups } from "./rejection-groups";
import { Sheet } from "./sheet";
import { Skeleton } from "./skeleton";
import { StateChip } from "./state-chip";

// ActivitySheet lists the user's recent runs, newest first, each with its
// state and opening to its outcome.
export function ActivitySheet() {
  const { isOpen, close, recent, statements } = useActivity();
  return (
    <Sheet
      open={isOpen}
      onClose={close}
      title="Activity"
      testId="activity-sheet"
    >
      {statements.isError && (
        <Notice tone="error" onRetry={() => statements.refetch()}>
          The activity could not be loaded.
        </Notice>
      )}
      {statements.data && recent.length === 0 && (
        <p className="text-sm text-text-muted">No uploads yet.</p>
      )}
      <ul className="flex flex-col gap-2">
        {recent.map((s) => (
          <Item key={s.run?.id} summary={s} onNavigate={close} />
        ))}
      </ul>
      <Link
        href="/statements"
        data-testid="activity-all"
        onClick={close}
        className="mt-auto pt-2 text-sm font-medium text-action underline-offset-4 hover:underline"
      >
        View all statements
      </Link>
    </Sheet>
  );
}

function Item({
  summary,
  onNavigate,
}: {
  summary: StatementSummary;
  onNavigate: () => void;
}) {
  const [open, setOpen] = useState(false);
  const id = summary.run?.id ?? "";
  return (
    <li
      data-testid={`activity-item-${id}`}
      className="rounded-md border border-border"
    >
      <button
        type="button"
        data-testid={`activity-toggle-${id}`}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-primary-light/15"
      >
        <ChevronRight
          aria-hidden
          className={`h-4 w-4 shrink-0 text-text-muted transition-transform ${open ? "rotate-90" : ""}`}
        />
        <span className="min-w-0 flex-1">
          <span className="block font-medium">
            {brokerLabel(summary.broker)} upload
          </span>
          <span className="block font-mono text-xs text-text-muted tabular-nums">
            {summary.run?.createdAt ? formatInstant(summary.run.createdAt) : ""}
          </span>
        </span>
        <StateChip summary={summary} />
      </button>
      {open && <Detail summary={summary} id={id} onNavigate={onNavigate} />}
    </li>
  );
}

function Detail({
  summary,
  id,
  onNavigate,
}: {
  summary: StatementSummary;
  id: string;
  onNavigate: () => void;
}) {
  const detail = useStatement(id);
  const o = outcome(summary);
  return (
    <div className="flex items-start gap-3 border-t border-border px-3 py-2 text-sm">
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1">
          <dt className="text-text-muted">From</dt>
          <dd className="font-mono tabular-nums">{summary.orderFrom}</dd>
          <dt className="text-text-muted">To</dt>
          <dd className="font-mono tabular-nums">
            {prevDay(summary.orderBefore)}
          </dd>
          <dt className="text-text-muted">Rows</dt>
          <dd className="font-mono tabular-nums">{summary.rows}</dd>
          <dt className="text-text-muted">Rejected</dt>
          <dd className="font-mono tabular-nums">{summary.rejected}</dd>
        </dl>
        {o === "failed" && summary.run?.error && (
          <p className="font-mono text-negative">{summary.run.error}</p>
        )}
        {o === "rejections" &&
          (detail.data ? (
            <RejectionGroups items={detail.data.items} rows={false} />
          ) : (
            <Skeleton lines={2} />
          ))}
      </div>
      <LinkButton
        href={`/statements/${id}`}
        data-testid={`activity-open-${id}`}
        onClick={onNavigate}
        className="shrink-0"
      >
        Open
        <ArrowRight aria-hidden className="h-4 w-4" />
      </LinkButton>
    </div>
  );
}
