"use client";

// Skeleton stands in for content that is still loading, with a bar per line
// so the layout does not jump when the content arrives.
export function Skeleton({ lines = 3 }: { lines?: number }) {
  return (
    <div
      data-testid="skeleton"
      aria-busy="true"
      className="flex animate-pulse flex-col gap-3"
    >
      {Array.from({ length: lines }, (_, i) => (
        <div
          key={i}
          className="h-4 rounded bg-border"
          style={{ width: `${70 - i * 15}%` }}
        />
      ))}
    </div>
  );
}
