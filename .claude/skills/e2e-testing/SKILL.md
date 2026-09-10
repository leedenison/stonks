---
name: e2e-testing
description: Conventions for Stonks end-to-end tests -- full-stack Playwright suites exercising Next.js, Envoy, the Go service, Postgres and Redis in Docker, against the production binary with sessions seeded directly rather than driven through Google. Covers stack layout, per-user isolation and what may run in parallel, what a spec may assert and through which surface, selectors and determinism. Use when adding or changing an e2e spec, the e2e stack or its fixtures, or when changing something the suite depends on such as a `data-testid` or the session record.
---

# End-to-End Testing

E2E tests drive a real browser against the whole stack. Run them with `make e2e-test`.

The suite lives in `e2e/`, its own npm project with its own lockfile and its own generated
protobuf types in `e2e/gen`.

## The stack

`docker/docker-compose.e2e.yml` overlays the base stack with shifted ports under the
compose project `stonks-e2e`.  Playwright runs as a compose service behind
`profiles: [test]`.

## Assertions

A spec asserts through the narrowest surface that can see the thing, in this order.

1. **The UI, for what the UI is responsible for** -- that the value reaches the screen,
   formatted correctly, in the right place. This is what e2e uniquely tests.
2. **The generated Connect client, for the data itself** -- quantities, scoping, derived
   values. The suite has protobuf-es types in `e2e/gen` and a seeded session, so it is an
   ordinary API client. 
3. **SQL, only for state the product deliberately does not surface** -- an audit row, an
   idempotency key, a soft-deleted record. Through a named helper beside the others, never
   inline in a spec, with a comment saying why the first two do not work.

Reaching for the database is usually a sign the assertion is in the wrong tier. Once a
value has made the round trip and rendered, the database has been proven to hold it.
Queries and derivations belong in the integration tier, against real Postgres with rollback
isolation, where the failure messages are better and the test is faster.

## External services

Nothing in a suite reaches a third party, and nothing in it replays recorded traffic.

E2E needs a provider to answer deterministically, not authentically. Authenticity is what
the integration tier's cassettes are for, and it is bought there with the client code that
actually has to parse what the provider sends.

## Shape of a spec

```ts
// Invented per spec, so no other spec competes over this instrument.
const TICKER = "ZZHOLD";

test.beforeAll(async () => { await seedInstrument(TICKER); });
test.afterAll(async () => { await closeDB(); await closeRedis(); });

test("shows the holdings", async ({ context, page }) => {
  const user = await seedUser();
  await seedHolding(user, TICKER, "120");
  await injectSession(context, await seedSession(user));
  await page.goto("/holdings");
  await expect(page.getByTestId(`holding-qty-${TICKER}`)).toHaveText("120");
});
```

## Determinism

The client honours a `data-testmode` attribute that zeroes animation and transition
durations. Prefer Playwright's auto-waiting assertions over explicit sleeps.

## See also

The `unit-testing` and `integration-testing` skills, the `frontend-design` skill for the
test id requirement, and the `typescript` skill for the style and formatting a spec follows.
