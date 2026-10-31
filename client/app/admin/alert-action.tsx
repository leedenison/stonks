"use client";

import { Button, LinkButton } from "@/app/components/button";
import { AttestIdentifierDialog } from "@/app/components/attest-identifier-dialog";
import { DecideCorpActDialog } from "@/app/components/decide-corp-act-dialog";
import {
  type AlertAction,
  type AlertRow,
  kindSlug,
  kindWords,
} from "@/lib/alerts";

// RowButton is the one action of an alert row.
export function RowButton({
  row,
  busy,
  onAct,
}: {
  row: AlertRow;
  busy: boolean;
  onAct: (a: AlertAction) => void;
}) {
  const testId = `alert-act-${kindSlug(row.kind)}-${row.key}`;
  const label = kindWords(row.kind).action;
  if (row.action.case === "open-run") {
    return (
      <LinkButton
        variant="secondary"
        data-testid={testId}
        href={`/admin/runs/${row.action.runId}`}
      >
        {label}
      </LinkButton>
    );
  }
  return (
    <Button
      variant={row.action.case === "attest" ? "primary" : "secondary"}
      data-testid={testId}
      disabled={busy}
      onClick={() => onAct(row.action)}
    >
      {label}
    </Button>
  );
}

// ActionDialog is the dialog for the actions that need one.
export function ActionDialog({
  action,
  onClose,
}: {
  action: AlertAction | null;
  onClose: () => void;
}) {
  if (action?.case === "attest") {
    return (
      <AttestIdentifierDialog
        ticker={action.ticker}
        from={action.from}
        to={action.to}
        onClose={onClose}
        onDone={onClose}
      />
    );
  }
  if (action?.case === "decide") {
    return (
      <DecideCorpActDialog corpActId={action.corpActId} onClose={onClose} />
    );
  }
  return null;
}
