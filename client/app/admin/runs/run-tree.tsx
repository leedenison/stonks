"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { Chip } from "@/app/components/chip";
import { SkeletonRows } from "@/app/components/skeleton-rows";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { Toggle } from "@/app/components/toggle";
import type { UserRun } from "@/gen/admin/v1/admin_pb";
import {
  filterQuery,
  openFindingsBelow,
  type RunFilters,
  runEnums,
  runTitle,
} from "@/lib/admin";
import { enumLabel } from "@/lib/enum";

// RunTreeTable shows runs nested under the run that started each. It shows
// placeholder rows while pending is set.
export function RunTreeTable({
  testId,
  pending = false,
  children,
}: {
  testId: string;
  pending?: boolean;
  children?: ReactNode;
}) {
  return (
    <TableCard testId={testId}>
      <Thead>
        <tr>
          <Th>Run</Th>
          <Th>Kind</Th>
          <Th>Trigger</Th>
          <Th>User</Th>
          <Th>State</Th>
          <Th>Findings</Th>
        </tr>
      </Thead>
      {pending ? <SkeletonRows columns={6} /> : <tbody>{children}</tbody>}
    </TableCard>
  );
}

// RunRow is one row of a RunTreeTable, indented by depth.
export function RunRow({
  run: r,
  depth,
  open,
  onToggle,
  current = false,
  muted = false,
  filters = {},
}: {
  run: UserRun;
  depth: number;
  // open is undefined when the row has no children. A closed row counts the
  // open findings of its hidden children with its own.
  open?: boolean;
  onToggle: () => void;
  // current marks the run the page describes. Its row is not a link and has
  // no hover style.
  current?: boolean;
  // muted dims a run that is listed only because a run below it matched.
  muted?: boolean;
  // filters are what the user link carries as it narrows the listing to
  // the run's user.
  filters?: Partial<RunFilters>;
}) {
  const id = r.run?.id ?? "";
  const href = `/admin/runs/${id}`;
  const title = runTitle(r);
  const findings = open === false ? openFindingsBelow(r) : r.openFindings;
  const cells = (
    <>
      <Td>
        <span
          className="flex items-center gap-1"
          style={{ paddingLeft: `${depth * 1.25}rem` }}
        >
          <Toggle open={open} onToggle={onToggle} testId={`run-toggle-${id}`} />
          {current ? (
            title
          ) : (
            <Link
              href={href}
              data-testid={`run-link-${id}`}
              className="underline-offset-4 hover:underline"
            >
              {title}
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
          href={filterQuery({ ...filters, before: "", user: r.userId })}
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
    <Tr
      href={href}
      data-testid={`run-row-${id}`}
      className={muted ? "text-text-muted" : ""}
    >
      {cells}
    </Tr>
  );
}
