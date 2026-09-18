"use client";

import type { ReactNode } from "react";

// Notice is the inline message above the content it concerns: an error the
// reader can act on, or a fact they should know before reading on. An error
// is announced; an info notice is not.
export function Notice({
  tone = "info",
  onRetry,
  testId,
  children,
}: {
  tone?: "error" | "info";
  onRetry?: () => void;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      data-testid={testId}
      className="flex items-start gap-3 rounded-md bg-accent-soft/50 px-3 py-2 text-sm text-on-accent-soft"
    >
      <p className="flex-1">{children}</p>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="shrink-0 font-semibold underline-offset-4 hover:underline"
        >
          Try again
        </button>
      )}
    </div>
  );
}
