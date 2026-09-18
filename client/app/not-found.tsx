"use client";

import { LinkButton } from "./components/button";

export default function NotFound() {
  return (
    <section data-testid="not-found-page" className="flex flex-col gap-3">
      <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-text-muted">There is nothing at this address.</p>
      <LinkButton data-testid="not-found-home" href="/" variant="secondary">
        Back to the start
      </LinkButton>
    </section>
  );
}
