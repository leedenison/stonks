"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { Run } from "@/gen/run/v1/run_pb";
import { useStartRefreshCorporateEvents } from "@/hooks/use-refresh-corporate-events";
import { refusal } from "@/lib/refusal";

// RefreshCorporateEventsDialog confirms and starts a refresh corporate events
// run over the tickers that named a held instrument. A refusal stays in the
// dialog.
export function RefreshCorporateEventsDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartRefreshCorporateEvents();
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Refresh corporate events"
      testId="refresh-corporate-events-dialog"
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
            data-testid="refresh-corporate-events-start"
            disabled={start.isPending}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Asks every enabled datasource again for the corporate events of each
        ticker that named a held instrument, without reading the cache. Each
        call uses up some of that datasource&apos;s quota.
      </p>
      {start.isError && (
        <Notice tone="error" testId="refresh-corporate-events-error">
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
