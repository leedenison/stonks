"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";

// BackButton leads to the page the current one belongs under, as a
// statement's page belongs under the statements. It is a fixed destination
// rather than the browser's history, so it reads the same however the page
// was reached.
export function BackButton({ to }: { to: string }) {
  return (
    <Link
      href={to}
      data-testid="page-back"
      aria-label="Back"
      className="rounded-md p-1 text-text-muted transition-colors hover:bg-primary-light/15 hover:text-text-primary"
    >
      <ArrowLeft aria-hidden className="h-5 w-5" />
    </Link>
  );
}
