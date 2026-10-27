"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { Run } from "@/gen/run/v1/run_pb";
import { useStartRebuildResolvedTickers } from "@/hooks/use-rebuild-resolved-tickers";
import { refusal } from "@/lib/refusal";

// RebuildResolvedTickersDialog confirms and starts a rebuild resolved tickers
// run over every ticker. A refusal stays in the dialog.
export function RebuildResolvedTickersDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartRebuildResolvedTickers();
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Rebuild ticker names"
      testId="rebuild-resolved-tickers-dialog"
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
            data-testid="rebuild-resolved-tickers-start"
            disabled={start.isPending}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Chooses again the ticker that names each instrument, from the facts
        already stored. It calls no datasource. Run it after a deploy that
        changes how names are chosen, or after facts were deleted.
      </p>
      {start.isError && (
        <Notice tone="error" testId="rebuild-resolved-tickers-error">
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
