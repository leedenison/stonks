"use client";

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
      <p className="font-mono text-sm text-text-muted">{error.message}</p>
      <button
        type="button"
        data-testid="error-retry"
        onClick={reset}
        className="w-fit rounded bg-primary px-3 py-1.5 text-sm font-medium text-on-primary"
      >
        Try again
      </button>
    </section>
  );
}
