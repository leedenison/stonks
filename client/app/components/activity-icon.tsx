"use client";

import { Activity } from "lucide-react";

// The top bar's entry to the activity sheet. The badge is the number of runs
// that finished since the sheet was last opened, and is hidden at zero.
export function ActivityIcon({
  count,
  onClick,
}: {
  count: number;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      data-testid="activity-icon"
      aria-label="Activity"
      onClick={onClick}
      className="relative rounded-lg p-2 text-on-dark/90 transition-colors hover:bg-on-dark/15"
    >
      <Activity aria-hidden className="h-5 w-5" />
      {count > 0 && (
        <span
          data-testid="activity-badge"
          className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-accent-dark px-1 text-center font-mono text-[10px] leading-4 font-semibold text-on-dark tabular-nums"
        >
          {count}
        </span>
      )}
    </button>
  );
}
