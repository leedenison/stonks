import { Info } from "lucide-react";

// InfoTip is an icon that shows text on a mouseover, for an explanation the
// page keeps out of its body. The browser draws the tip, so a scrolling card
// does not clip it. A keyboard reaches the icon and a screen reader reads the
// text.
export function InfoTip({ text, testId }: { text: string; testId?: string }) {
  return (
    <span
      role="img"
      tabIndex={0}
      title={text}
      aria-label={text}
      data-testid={testId}
      className="inline-flex cursor-help align-middle text-text-muted hover:text-text-primary focus-visible:text-text-primary focus-visible:outline-none"
    >
      <Info aria-hidden className="h-3.5 w-3.5" />
    </span>
  );
}
