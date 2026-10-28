---
title: Stored ticker validity with one identifier event source per market
type: task
---

## Scope

Ticker validity is stored, and one table answers what a ticker names on a day. Each
market has at most one identifier event source. See
[015](../adr/015-a-market-has-one-identifier-event-source.md) and
[016](../adr/016-ticker-validity-is-stored.md).

In:

- A `ticker_validity` table. Each write of facts rebuilds the validity of its tickers
  before it commits.
- Every reader of what a ticker names on a day reads the table: holdings, statement marks,
  corporate action attribution and coverage, `refreshcorpacts`, the dated path of a
  resolution, re-assertion of held tickers and the admin Tickers tab.
- A `txn_instrs` view gives each transaction its instrument and validity, whether its key
  is fixed or dated.
- Derivation, and the reading of facts for it, are private to `tickervalidity`.
- `markets.identifier_event_source`. Edges and assertion coverage count only from it.
  Chain requests go only to it.
- Delete the `overruled_edge` finding kind.
- Name the rebuild run `rebuild_ticker_validity`.
- Delete `market.Ranged`, `market.NextPeriod`, `Entry.Lag` and `datasources.lag_days`.
  No integration uses them.

Out:

- Setting a market's identifier event source in the admin UI. It is set in
  `local/seed.sql`, the e2e setup and test fixtures.
- Any change to how points alone establish ticker validity in a market without an
  identifier event source.
