"use client";

import { Button } from "./components/button";
import { Notice } from "./components/notice";

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <section data-testid="error-page" className="flex flex-col gap-3">
      <h1 className="text-2xl font-semibold tracking-tight">
        Something went wrong
      </h1>
      <Notice tone="error">
        <span className="font-mono">{error.message}</span>
      </Notice>
      <Button data-testid="error-retry" onClick={reset}>
        Try again
      </Button>
    </section>
  );
}
