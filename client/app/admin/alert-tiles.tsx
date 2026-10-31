"use client";

import Link from "next/link";
import type { GetAlertDigestResponse } from "@/gen/admin/alert/v1/alert_pb";
import { type AlertParams, kindSlug, kindWords, openCount } from "@/lib/alerts";

// AlertTiles shows the open count of all kinds and of each kind. A tile
// narrows the table to its kind. The open tile returns to the digest.
export function AlertTiles({
  digest,
  p,
}: {
  digest: GetAlertDigestResponse;
  p: AlertParams;
}) {
  return (
    <div data-testid="alert-tiles" className="flex flex-wrap gap-2">
      <Tile
        href="/admin"
        testId="alert-tile-open"
        on={p.kind === undefined}
        n={openCount(digest)}
        label="open"
      />
      {digest.groups.map((g) => (
        <Tile
          key={g.kind}
          href={`/admin?kind=${kindSlug(g.kind)}`}
          testId={`alert-tile-${kindSlug(g.kind)}`}
          on={p.kind === g.kind}
          n={g.count}
          label={kindWords(g.kind).tile}
        />
      ))}
    </div>
  );
}

function Tile({
  href,
  testId,
  on,
  n,
  label,
}: {
  href: string;
  testId: string;
  on: boolean;
  n: number;
  label: string;
}) {
  return (
    <Link
      href={href}
      data-testid={testId}
      aria-current={on ? "true" : undefined}
      className={`flex items-baseline gap-2 rounded-md border bg-surface px-3 py-2 transition-colors hover:bg-primary-light/15 ${
        on ? "border-primary ring-1 ring-primary" : "border-border"
      }`}
    >
      <span
        className={`font-mono text-xl tabular-nums ${n === 0 ? "font-normal text-text-muted" : "font-semibold"}`}
      >
        {n}
      </span>
      <span className="text-sm text-text-muted">{label}</span>
    </Link>
  );
}
