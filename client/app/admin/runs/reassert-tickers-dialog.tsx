"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { Run } from "@/gen/run/v1/run_pb";
import { useStartReassertTickers } from "@/hooks/use-reassert-tickers";
import { refusal } from "@/lib/refusal";

// ReassertTickersDialog confirms and starts a reassert tickers run over every
// held ticker. A refusal stays in the dialog.
export function ReassertTickersDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartReassertTickers();
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Reassert tickers"
      testId="reassert-tickers-dialog"
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
            data-testid="reassert-tickers-start"
            disabled={start.isPending}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Asks every enabled datasource again about every held ticker, to confirm
        that each ticker still names the same instrument today. Each call uses
        up some of that datasource&apos;s quota.
      </p>
      {start.isError && (
        <Notice tone="error" testId="reassert-tickers-error">
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
