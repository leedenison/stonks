"use client";

import Link from "next/link";

export default function NotFound() {
  return (
    <section data-testid="not-found-page" className="flex flex-col gap-3">
      <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-text-muted">There is nothing at this address.</p>
      <Link
        data-testid="not-found-home"
        href="/"
        className="w-fit text-primary underline-offset-4 hover:underline"
      >
        Back to the start
      </Link>
    </section>
  );
}
