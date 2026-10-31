"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useState } from "react";
import { Button } from "@/app/components/button";
import { InfoTip } from "@/app/components/info-tip";
import { Section } from "@/app/components/section";
import { Skeleton } from "@/app/components/skeleton";
import { RunChip } from "@/app/components/state-chip";
import { TableCard, Td, Th, Thead, Tr } from "@/app/components/table";
import { useLastSystemRun } from "@/hooks/use-admin-runs";
import {
  StartRunDialog,
  type SystemRun,
  systemRunGroups,
} from "./start-run-dialog";

// RunsCard lists the system runs, each with its last run and a button that
// starts another. A started run opens on its own page.
export function RunsCard() {
  const router = useRouter();
  const [opened, setOpened] = useState<SystemRun | null>(null);

  return (
    <Section title="Runs">
      <TableCard testId="runs-card">
        <Thead>
          <tr>
            <Th>Run</Th>
            <Th>Last run</Th>
            <Th> </Th>
          </tr>
        </Thead>
        <tbody>
          {systemRunGroups.map((g) => (
            <Fragment key={g.heading ?? ""}>
              {g.heading && (
                <tr data-testid={`runs-card-group-${g.heading.toLowerCase()}`}>
                  <td
                    colSpan={3}
                    className="border-b border-border bg-surface-tint px-4 py-1.5 text-xs font-semibold tracking-wider text-text-muted uppercase"
                  >
                    {g.heading}
                  </td>
                </tr>
              )}
              {g.runs.map((r) => (
                <RunsCardRow
                  key={r.kind}
                  run={r}
                  onStart={() => setOpened(r)}
                />
              ))}
            </Fragment>
          ))}
        </tbody>
      </TableCard>
      {opened && (
        <StartRunDialog
          run={opened}
          onClose={() => setOpened(null)}
          onStarted={(started) => {
            setOpened(null);
            router.push(`/admin/runs/${started.id}`);
          }}
        />
      )}
    </Section>
  );
}

function RunsCardRow({
  run,
  onStart,
}: {
  run: SystemRun;
  onStart: () => void;
}) {
  const last = useLastSystemRun(run.kind);
  const id = last.data?.run?.id;
  return (
    <Tr data-testid={`runs-card-row-${run.testId}`}>
      <Td>
        <span className="inline-flex items-center gap-2 font-semibold">
          {run.title}
          <InfoTip text={run.summary} testId={`runs-card-why-${run.testId}`} />
        </span>
      </Td>
      <Td data-testid={`runs-card-last-${run.testId}`}>
        {last.isPending ? (
          <Skeleton lines={1} />
        ) : last.isError ? (
          <span className="text-text-muted">Unknown</span>
        ) : id ? (
          <Link href={`/admin/runs/${id}`}>
            <RunChip run={last.data?.run} />
          </Link>
        ) : (
          <span className="text-text-muted">Never run</span>
        )}
      </Td>
      <Td>
        <Button
          variant="secondary"
          data-testid={`runs-card-start-${run.testId}`}
          onClick={onStart}
        >
          Start
        </Button>
      </Td>
    </Tr>
  );
}
