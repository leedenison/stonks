"use client";

import { LinkButton } from "./components/button";

export default function NotFound() {
  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <section data-testid="not-found-page" className="flex flex-col gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">
          Page not found
        </h1>
        <p className="text-text-muted">Not found.</p>
        <LinkButton data-testid="not-found-home" href="/" variant="secondary">
          Back to the start
        </LinkButton>
      </section>
    </main>
  );
}
