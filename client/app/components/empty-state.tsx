import type { ReactNode } from "react";

// EmptyState stands where a table would, saying what it would hold and
// offering the action that fills it.
export function EmptyState({
  message,
  action,
}: {
  message: string;
  action?: ReactNode;
}) {
  return (
    <div
      data-testid="empty-state"
      className="flex flex-col items-center gap-3 rounded-md border border-dashed border-border bg-surface px-6 py-12 text-center"
    >
      <p className="text-sm text-text-muted">{message}</p>
      {action}
    </div>
  );
}
