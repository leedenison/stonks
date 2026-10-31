"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import { type Run, RunKind } from "@/gen/run/v1/run_pb";
import { useStartSystemRun } from "@/hooks/use-start-system-run";
import { refusal } from "@/lib/refusal";

// The system runs an administrator can start, and the dialog that confirms
// and starts one.

// A SystemRun is one kind of run an administrator starts by hand.
export type SystemRun = {
  kind: RunKind;
  // Prefix of the test ids of the run's row, its dialog and their controls.
  testId: string;
  title: string;
  // One sentence on what the run does.
  summary: string;
  // Each element is one paragraph of the dialog.
  description: string[];
};

// A SystemRunGroup is the rows under one heading of the runs card.
export type SystemRunGroup = {
  // Absent for the first group, which has no heading.
  heading?: string;
  runs: SystemRun[];
};

// systemRunGroups lists the system runs in the order of the runs card.
export const systemRunGroups: SystemRunGroup[] = [
  {
    runs: [
      {
        kind: RunKind.REASSERT_TICKERS,
        testId: "reassert-tickers",
        title: "Reassert All Tickers",
        summary:
          "Asks every enabled datasource again about every held ticker and replays the statement keys that state them.",
        description: [
          "Asks every enabled datasource again about every held ticker, without reading the cache. A datasource that serves identifier facts is asked for the history of the instrument the ticker names. Any other datasource is asked which instrument the ticker names today.",
          "The answers are written as identifier facts and complete any findings that wait on them. The run then replays the unresolved statement keys that state each ticker. It also fetches the corporate actions of the held instruments that the tickers name.",
          "A ticker that becomes held after the run starts waits for the next run. Each call spends quota on the datasource it asks.",
        ],
      },
      {
        kind: RunKind.REFRESH_CORP_ACTS,
        testId: "refresh-corp-acts",
        title: "Refresh Corporate Actions",
        summary:
          "Fetches the corporate actions of every held instrument again from the source of its market.",
        description: [
          "Asks the corporate actions source of each market again about each ticker there that named a held instrument, without reading the cache. A held instrument is one that a transaction names. The run does not ask a datasource about ticker identity.",
          "Each request covers the period from the earliest transaction of any user that names the instrument to tomorrow. Its answer replaces the corporate actions stored for that period.",
          "The requests are chosen when the run starts. An instrument that becomes held later waits for the next run. Each call spends quota on the source it asks.",
        ],
      },
    ],
  },
  {
    heading: "Debug",
    runs: [
      {
        kind: RunKind.REBUILD_TICKER_VALIDITY,
        testId: "rebuild-ticker-validity",
        title: "Remap Ticker Names",
        summary:
          "Rebuilds ticker validity and names from the stored identifier facts without calling a datasource.",
        description: [
          "Rebuilds the ticker validity and the names of every ticker from the identifier facts already stored. It does not call a datasource.",
          "A write of facts rebuilds its own tickers, and a reorder of the datasources starts this run. Run it after facts were deleted, or after a deploy that changes how validity or names are derived.",
          "Ticker names and validity may lag until the run finishes.",
        ],
      },
    ],
  },
];

// systemRuns lists every run of systemRunGroups.
export const systemRuns = systemRunGroups.flatMap((g) => g.runs);

// StartRunDialog confirms and starts a system run. A refusal stays in the
// dialog.
export function StartRunDialog({
  run,
  onClose,
  onStarted,
}: {
  run: SystemRun;
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartSystemRun(run.kind);
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title={run.title}
      testId={`${run.testId}-dialog`}
      footer={
        <>
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={start.isPending}
          >
            Cancel
          </Button>
          <Button
            data-testid={`${run.testId}-start`}
            disabled={start.isPending}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <div className="space-y-3 text-sm">
        {run.description.map((p) => (
          <p key={p}>{p}</p>
        ))}
      </div>
      {start.isError && (
        <Notice tone="error" testId={`${run.testId}-error`}>
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
