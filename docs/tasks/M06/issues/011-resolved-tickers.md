---
title: Resolved tickers
type: task
dependencies: [010]
---

## Scope

An instrument is named by a projection of ticker facts. See
[008](../adr/008-a-ticker-assertion-records-the-venue-and-currency-its-source-stated.md),
[011](../adr/011-an-instruments-ticker-is-a-projection-of-ticker-facts.md) and
[012](../adr/012-a-fact-write-holds-its-ticker-rows-until-commit.md).

In:

- `venue` and `currency` on `identifier_assertions`, written by the identity write and
  by OpenFIGI from its currency filter.
- The `resolved_tickers` table and its rule, in `tickervalidity`, reusing `ReadTickers`
  and `Derive`.
- The rebuild in the identity write, `Record`, an identifier attestation or its
  withdrawal, and a merge, under the locks of
  [012](../adr/012-a-fact-write-holds-its-ticker-rows-until-commit.md).
- The `rebuild_resolved_tickers` run kind, in batches. A change of precedence starts it,
  and so does an admin action.
- The views `listing_tickers` and `instr_names` read the projection and decide the
  relation on read.
- `ListListingNames`, `ListInstrTickers`, the admin search and the dated key
  listing fallback (`listing` in `service/holding/holding.go`) read the projection.
- Proto:
  - A relation enum (current, defunct, former) replaces `InstrTicker.current`.
  - `HoldingListing` carries a `MarketTicker` and an optional venue in place of a
    `mic_ticker` identifier.
- The client shows `(defunct)`.
- Comments: "Ticker validity is never stored" in `tickervalidity.go` is narrowed. The
  projection stores names.

## Tests

- Rule tests: the latest interval, a tie on an open interval, the market-level row, and
  the defunct and former relations.
- A dbtest over the views:
  - defunct against former;
  - a row with a NULL venue shows its market;
  - a row with a NULL currency reaches no listing;
  - a listing that a later write creates is reached without a rebuild.
- A lock dbtest. Two transactions run on two pool connections. Both roll back. The
  first holds a ticker row lock. The second sets a short `lock_timeout` and fails with
  `lock_not_available`.
- Rebuild dbtests for each writer:
  - the identity write leaves a row with the answer's venue and currency;
  - the Massive record leaves a row;
  - `TestAttest`: after B takes a ticker from A, an identifier attestation that the ticker
    names A ends B's row, and B becomes defunct;
  - a merge rebuilds both instruments.
- A dbtest of the run across more than one batch.
- `datasource_test.go`: a change of precedence starts the run.
- Service and client tests for the admin action.
- `admin-datasources.spec.ts` asserts that the precedence change started the runs, and
  waits for them to finish.
- An e2e spec in which a reassigned ticker shows `(defunct)` on its old holding:
  - It uses a security that only this spec states.
  - Its fixture states the old holder by a stable identifier, so the holding is an
    instrument.
  - It needs new `make e2e-record` interactions.
  - It asserts the relation over Connect and the suffix in the UI.
