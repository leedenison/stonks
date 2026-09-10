---
name: integration-testing
description: Conventions for Stonks integration tests -- the database and session layers against real Postgres, TimescaleDB and Redis in an isolated container stack, and clients of external HTTP services against traffic recorded with go-vcr. Use when a test needs a real Postgres or Redis, when a test replays or re-records a cassette, or when deciding whether a test belongs in this tier rather than as a unit test.
---

# Integration Testing

Integration tests cover the code that talks to something real rather than a mock: the
generated queries against real Postgres with TimescaleDB, the session store against real
Redis, and clients of external HTTP services against recorded traffic.

The first two need the test stack and run with `make db-test`. Replaying a recording needs
nothing, and is covered under External services below.

## The stack

`docker/docker-compose.test.yml` is standalone, not an overlay: Postgres on 5433, Redis on
6380, and a `tester` service sharing their network. Its compose project is `stonks-test`.

## Selection

A test that needs the stack carries the `dbtest` build tag, so it does not compile into a
plain `go test` and cannot reach real infrastructure from one:

```go
//go:build dbtest

package db_test
```

`make db-test` runs the packages holding a tagged file. They are discovered by the
Makefile, so a test in a new package needs no change there.

`STONKS_TEST_DATABASE_URL` and `STONKS_TEST_REDIS_URL` configure the stack rather than
select the tests. They are set only inside it, and a `TestMain` whose URL is missing fails
the run, so a stack a test cannot reach is a red run rather than a green one that tested
nothing.

## Isolation

**Each test runs inside a transaction that is rolled back.** The store is constructed over
the transaction rather than the pool, so everything the test writes disappears when it
ends, and tests neither see each other's data nor need an ordering.

Do not clean up by deleting rows. A test that half-fails leaves its own mess behind.

Migrations are applied once when the stack starts. Reference data seeded by a migration is
present in every test and must not be deleted by one.

## External services

Code that calls an external HTTP service is tested against recorded traffic, replayed with
[go-vcr](https://github.com/dnaeon/go-vcr). The recording is made once against the real
service and committed; every run after that is a replay.

These tests have two purposes:

- An explicit expression of the external service behaviour the system was built against.
  If the external service behaviour changes unexpectedly it is easy to determine the diff
  by comparing to the behaviour of the tests.
- Where an internal API boundary exists abtracting the external service, these tests
  verify that the translation from external service responses to API abtraction are 
  executed correctly.

Replay needs no credentials, no network, no stack and no tag, so these tests are untagged
and run under `make server-test`.

Cassettes are YAML under `testdata/` beside the test that plays them, one per scenario,
named for the case rather than the endpoint -- `price_not_found.yaml`, not `get_v1_eod.yaml`.
Base names are unique across the repository, which is what lets the record target name one.

### The helper

`server/internal/testutil/vcr` holds the recorder. A test asks for a client over a cassette,
and declares in a `Scrub` what its provider's traffic carries:

```go
var scrub = vcr.Scrub{
    Query:           []string{"api_token"},
    ResponseHeaders: []string{"X-Provider-Trace"},
    Body:            func(b string) string { return accountRe.ReplaceAllString(b, vcr.Placeholder) },
}

key := vcr.Credential(t, "PROVIDER_API_KEY", "testdata/price_not_found")
client := vcr.New(t, "testdata/price_not_found", scrub)
```

Request headers are dropped by allowlist without the client saying anything, so a credential
in an unfamiliar header cannot reach disk. **`Scrub.Body` is mandatory**: a client whose
bodies carry nothing to remove declares `vcr.NoScrub`, so no provider is recorded without
the question having been asked of it.

`Credential` returns the environment variable while recording and `vcr.Placeholder`
otherwise. A provider carrying its key in the query string is therefore asked, on replay,
for the URL the redacted cassette holds, which is the only way such an interaction can match.

**An unmatched request fails the test.** A request the cassette does not cover must error
rather than fall through to a live call.

Match on method, URL, and the body where it is what distinguishes one request from another.
Do not match on headers: dates, nonces and authorization differ between the recording and
the replay.

### Re-record one cassette at a time

**`make record CASSETTE=<name>` takes the cassette to make and re-records only that one.**
The whole suite runs, every other cassette replays, and the provider's credentials come
from the environment.

Provider's responses can drift underneath a suite because the same request made at a
different time genuinely elicits a different response. Refreshing every cassette therefore
requires new test cases to be found to exercise todays responses.  Instead undisturbed test
cases run as though they were being run at the time of recording (see Cassettes freeze
time). 

### Cassettes freeze time

A recording carries the timestamps of the moment it was made, and they recede. Anything the
code compares against the current time goes stale.  The code under test takes a clock and
the test pins it to a time the cassette is consistent with. 

## What to test here

* Code that depends on external services (see External services).
* Code whose primary effect is the creation of data in a datastore.
* Code that exists in the database (plsql, triggers, complex queries, CTEs, etc).

Note: Simple selects, inserts and updates, where the signature of the function closely 
matches the column spec of a table, should not be tested (similar to how its not worth
testing getters and setters).

## See also

The `unit-testing` skill for the clock rule these tests depend on, and the `e2e-testing`
skill.
