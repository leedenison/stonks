import { LinkButton } from "./button";

export function AccessDenied() {
  return (
    <section
      data-testid="access-denied"
      className="flex flex-col items-center gap-3 py-16 text-center"
    >
      <h1 className="text-2xl font-semibold tracking-tight">Access denied</h1>
      <p className="text-text-muted">Access denied.</p>
      <LinkButton href="/transactions" variant="secondary">
        Back to the transactions
      </LinkButton>
    </section>
  );
}
