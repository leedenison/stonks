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

The service is the binary that ships, built from its production Dockerfile, and
configuration is the only difference between this stack and any other. It carries no
test-only endpoint, switch or hook. State a test needs to observe is exposed on the real
API: if a test wants it an operator usually wants it too, so it is built as a feature with
a supported read path.

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

Nothing reaches a third party during a test run. The provider is `vcrproxy`, a reverse
proxy in the e2e overlay that replays traffic recorded with go-vcr, named as the
datasource by the suite's global setup. `make e2e-record` forwards what the recording
lacks to the real provider and appends what comes back; every other run replays. The
cassettes live in `docker/vcrproxy/`: `recorded.yaml` holds only what the provider sent,
and `authored.yaml` the few hand-written interactions, such as rate limit refusals, that
cannot be recorded.

A spec picks its case by the identifier its fixture states, so specs stay independent and
the suite parallel. A fixture states a real public identifier where a listing is wanted,
a different security per spec, so no spec's lookup finds an instrument another spec's
fetch created. The integration tier owns what the parser makes of the provider's
responses; e2e asserts what the system does with the outcome.

## Shape of a spec

`test` comes from `e2e/helpers/test.ts`, whose fixtures seed the users a test needs:
`signIn(role)` seeds a user and puts their session in the browser context, and
`seed(role)` seeds one without. What a test seeds is removed when it ends, and the
database and Redis connections close with the worker.

```ts
// Invented per spec, so no other spec competes over this instrument.
const TICKER = "ZZHOLD";

test.beforeAll(async () => { await seedInstrument(TICKER); });

test("shows the holdings", async ({ signIn, page }) => {
  const { user } = await signIn();
  await seedHolding(user, TICKER, "120");
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
