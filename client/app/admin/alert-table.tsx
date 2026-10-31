"use client";

import Link from "next/link";
import { Chip } from "@/app/components/chip";
import {
  IdentifierChip,
  MarketTickerChip,
} from "@/app/components/identifier-chip";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import type { Alert, AlertGroup } from "@/gen/admin/alert/v1/alert_pb";
import {
  type AlertAction,
  type AlertItem,
  type AlertRow,
  alertRow,
  kindSlug,
  kindWords,
} from "@/lib/alerts";
import { RowButton } from "./alert-action";

const columns = 7;

// Group is one kind's alerts under a heading row. Count is the kind's total,
// which can exceed the alerts shown.
export type Group = {
  kind: AlertGroup["kind"];
  count: number;
  alerts: Alert[];
};

// AlertTable lists groups of alerts, each under a heading row.
export function AlertTable({
  groups,
  pending,
  busy,
  onAct,
  more,
}: {
  groups: Group[];
  pending?: boolean;
  busy: boolean;
  onAct: (a: AlertAction) => void;
  // Whether each group ends with a row that opens the kind's full list.
  more: boolean;
}) {
  return (
    <TableCard testId="alerts-table">
      <Thead>
        <tr>
          <Th>Item</Th>
          <Th>Detail</Th>
          <Th numeric>Txns</Th>
          <Th numeric>Keys</Th>
          <Th numeric>Since</Th>
          <Th> </Th>
        </tr>
      </Thead>
      {pending ? (
        <SkeletonRows columns={6} />
      ) : (
        <tbody>
          {groups.map((g) => (
            <GroupRows
              key={g.kind}
              g={g}
              busy={busy}
              onAct={onAct}
              more={more}
            />
          ))}
        </tbody>
      )}
    </TableCard>
  );
}

function GroupRows({
  g,
  busy,
  onAct,
  more,
}: {
  g: Group;
  busy: boolean;
  onAct: (a: AlertAction) => void;
  more: boolean;
}) {
  const w = kindWords(g.kind);
  const slug = kindSlug(g.kind);
  return (
    <>
      <tr data-testid={`alert-group-${slug}`}>
        <td
          colSpan={columns}
          className="border-b border-border bg-surface-tint px-4 py-1.5 text-xs font-semibold tracking-wider text-text-muted uppercase"
        >
          {w.group}
          <span className="ml-2 font-mono font-normal">{g.count}</span>
          <span className="ml-4 font-normal tracking-normal normal-case">
            {w.why}
          </span>
        </td>
      </tr>
      {g.alerts.map(alertRow).map((r) => (
        <Row key={r.key} r={r} busy={busy} onAct={onAct} />
      ))}
      {more && g.count > g.alerts.length && (
        <tr>
          <td
            colSpan={columns}
            className="border-b border-border px-4 py-2 text-sm"
          >
            <Link
              data-testid={`alert-more-${slug}`}
              href={`/admin?kind=${slug}`}
              className="text-action underline-offset-4 hover:underline"
            >
              Show all {g.count} {w.tile}
            </Link>
          </td>
        </tr>
      )}
    </>
  );
}

function Row({
  r,
  busy,
  onAct,
}: {
  r: AlertRow;
  busy: boolean;
  onAct: (a: AlertAction) => void;
}) {
  return (
    <Tr data-testid={`alert-row-${kindSlug(r.kind)}-${r.key}`}>
      <Td>
        <ItemCell item={r.item} />
      </Td>
      <Td>
        {r.detail}
        {r.sub && (
          <span className="mt-0.5 block text-[0.8125rem] text-text-muted">
            {r.sub}
          </span>
        )}
      </Td>
      <Td numeric>{r.txns ?? "—"}</Td>
      <Td numeric>{r.keys ?? "—"}</Td>
      <Td numeric className="whitespace-nowrap">
        {r.since}
      </Td>
      <Td className="whitespace-nowrap">
        <RowButton row={r} busy={busy} onAct={onAct} />
      </Td>
    </Tr>
  );
}

function ItemCell({ item }: { item: AlertItem }) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {item.ticker && <MarketTickerChip ticker={item.ticker} />}
      {item.label &&
        (item.href ? (
          <Link
            href={item.href}
            className="font-mono text-action underline-offset-4 hover:underline"
          >
            {item.label}
          </Link>
        ) : (
          <span>{item.label}</span>
        ))}
      {item.tag && <Chip>{item.tag}</Chip>}
      {item.sent && <IdentifierChip id={item.sent} />}
    </div>
  );
}
