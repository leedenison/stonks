"use client";

import type { ReactNode } from "react";

// Notice is the inline message above the content it concerns: an error that
// asks the reader to act, a fact they should know before reading on, or the
// news that something succeeded in full. An error is announced at once, and
// any other notice when the reader is idle.
export function Notice({
  tone = "info",
  onRetry,
  testId,
  children,
}: {
  tone?: "error" | "info" | "positive";
  onRetry?: () => void;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      data-testid={testId}
      data-tone={tone}
      className={`flex items-start gap-3 rounded-md px-3 py-2 text-sm ${
        tone === "positive"
          ? "bg-positive/10 text-positive"
          : "bg-accent-soft/50 text-on-accent-soft"
      }`}
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
