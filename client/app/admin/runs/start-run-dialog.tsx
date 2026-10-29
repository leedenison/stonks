"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import { type Run, RunKind } from "@/gen/run/v1/run_pb";
import { useStartSystemRun } from "@/hooks/use-start-system-run";
import { refusal } from "@/lib/refusal";

// A SystemRun is an action that starts one kind of system run.
export type SystemRun = {
  kind: RunKind;
  // Prefix of the test ids of the action button, the dialog and its controls.
  testId: string;
  title: string;
  description: string;
};

// systemRuns lists the system runs an administrator can start.
export const systemRuns: SystemRun[] = [
  {
    kind: RunKind.REASSERT_TICKERS,
    testId: "reassert-tickers",
    title: "Reassert tickers",
    description:
      "Asks every enabled datasource again about every held ticker, to confirm that each ticker still names the same instrument today. Each call spends quota on the datasource it asks.",
  },
  {
    kind: RunKind.REFRESH_CORP_ACTS,
    testId: "refresh-corp-acts",
    title: "Refresh corporate actions",
    description:
      "Asks the corporate actions source of each market again about each ticker there that named a held instrument, without reading the cache. Each call spends quota on the source it asks.",
  },
  {
    kind: RunKind.REBUILD_TICKER_VALIDITY,
    testId: "rebuild-ticker-validity",
    title: "Rebuild ticker validity",
    description:
      "Rebuilds the ticker validity and the names of every ticker from the facts already stored. It does not call a datasource. Run it after a deploy that changes how validity or names are derived, or after facts were deleted.",
  },
];

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
      <p className="text-sm">{run.description}</p>
      {start.isError && (
        <Notice tone="error" testId={`${run.testId}-error`}>
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
