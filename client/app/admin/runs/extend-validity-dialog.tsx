"use client";

import { Button } from "@/app/components/button";
import { Dialog } from "@/app/components/dialog";
import { Notice } from "@/app/components/notice";
import type { Run } from "@/gen/run/v1/run_pb";
import { useStartExtendValidity } from "@/hooks/use-extend-validity";
import { refusal } from "@/lib/refusal";

// ExtendValidityDialog confirms and starts an extend validity run over every
// held ticker. A refusal stays in the dialog.
export function ExtendValidityDialog({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const start = useStartExtendValidity();
  const submit = () =>
    start.mutate(undefined, {
      onSuccess: (res) => res.run && onStarted(res.run),
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Extend validity"
      testId="extend-validity-dialog"
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
            data-testid="extend-validity-start"
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
        <Notice tone="error" testId="extend-validity-error">
          {refusal(start.error, "The run could not be started.")}
        </Notice>
      )}
    </Dialog>
  );
}
