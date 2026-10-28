"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { Run } from "@/gen/run/v1/run_pb";
import { useStartRebuildTickerValidity } from "@/hooks/use-rebuild-ticker-validity";
import { refusal } from "@/lib/refusal";

// RebuildTickerValidityDialog confirms and starts a rebuild ticker validity
// run over every ticker. A refusal stays in the dialog.
export function RebuildTickerValidityDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartRebuildTickerValidity();
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Rebuild ticker validity"
      testId="rebuild-ticker-validity-dialog"
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
            data-testid="rebuild-ticker-validity-start"
            disabled={start.isPending}
            onClick={submit}
          >
            Start
          </Button>
        </>
      }
    >
      <p className="text-sm">
        Rebuilds the ticker validity and the names of every ticker from the
        facts already stored. It does not call a datasource. Run it after a
        deploy that changes how validity or names are derived, or after facts
        were deleted.
      </p>
      {start.isError && (
        <Notice tone="error" testId="rebuild-ticker-validity-error">
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
