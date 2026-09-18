"use client";

import { useRouter } from "next/navigation";
import type { ComponentProps, ReactNode } from "react";

// A table sits in a card that scrolls in both directions within the page,
// so its header stays put while the rows scroll under it and wide columns
// scroll sideways. Numbers are right aligned in the mono face.
export function TableCard({
  testId,
  children,
}: {
  testId?: string;
  children: ReactNode;
}) {
  return (
    <div className="max-h-[calc(100dvh-var(--top-bar-height)-11rem)] overflow-auto rounded-md border border-border bg-surface shadow-xs">
      <table data-testid={testId} className="w-full border-collapse text-sm">
        {children}
      </table>
    </div>
  );
}

export function Thead({ children }: { children: ReactNode }) {
  return <thead className="sticky top-0 z-10">{children}</thead>;
}

export function Th({
  numeric,
  className = "",
  ...rest
}: ComponentProps<"th"> & { numeric?: boolean }) {
  return (
    <th
      className={`border-b-2 border-primary-dark/10 bg-surface-tint px-4 py-3 text-xs font-semibold tracking-wider text-text-muted uppercase ${numeric ? "text-right" : "text-left"} ${className}`}
      {...rest}
    />
  );
}

export function Td({
  numeric,
  className = "",
  ...rest
}: ComponentProps<"td"> & { numeric?: boolean }) {
  return (
    <td
      className={`border-b border-border px-4 py-3 ${numeric ? "text-right font-mono tabular-nums" : ""} ${className}`}
      {...rest}
    />
  );
}

// Tr with an href is a row that opens a page. The row itself navigates on
// a click, and its first cell holds the link so a keyboard reaches it.
export function Tr({
  href,
  className = "",
  ...rest
}: ComponentProps<"tr"> & { href?: string }) {
  const router = useRouter();
  return (
    <tr
      onClick={href ? () => router.push(href) : undefined}
      className={`transition-colors hover:bg-primary-light/15 ${href ? "cursor-pointer" : ""} ${className}`}
      {...rest}
    />
  );
}
