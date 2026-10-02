import { Minus, Plus } from "lucide-react";

// Toggle opens and closes the rows below a table row. open is undefined
// where the row has nothing below it, which keeps the column aligned with
// a blank of the same size. A click reaches the button alone.
export function Toggle({
  open,
  onToggle,
  testId,
}: {
  open?: boolean;
  onToggle: () => void;
  testId: string;
}) {
  if (open === undefined) {
    return <span className="size-4" aria-hidden="true" />;
  }
  const Icon = open ? Minus : Plus;
  return (
    <button
      type="button"
      aria-expanded={open}
      aria-label={open ? "Close" : "Open"}
      data-testid={testId}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      className="rounded border border-border text-text-muted hover:text-text-primary"
    >
      <Icon className="size-4" aria-hidden="true" />
    </button>
  );
}
