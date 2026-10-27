---
title: Tickers and markets as tables
type: task
dependencies: [017]
---

## Scope

Every reference to a ticker becomes a foreign key to `tickers`, and every market a
foreign key to `markets`. See
[007](../adr/007-identifiers-hold-identity-and-a-ticker-holds-only-its-spelling.md) and
the market columns of
[008](../adr/008-a-ticker-assertion-records-the-venue-and-currency-its-source-stated.md).

In:

- Migrations, edited in place:
  - `markets`, a complete `market_members` with `UNIQUE (operating_mic, market)`, and
    `mic_markets` as a join.
  - `tickers (id, market, value)`.
  - `identifier_assertions` gains `ticker_id` and `market`, behind the composite foreign
    key to `tickers`. Its type, domain and value go.
  - `identifier_events` gains `ticker_id` and `to_ticker_id`.
  - `identifier_event_coverage` gains a nullable `ticker_id`, behind a composite foreign
    key on `(ticker_id, market)`.
  - `corp_act_answers` gains `ticker_id`, with `UNIQUE (datasource, ticker_id)`.
    `corp_acts` is unchanged. It reaches its ticker through `answer_id`.
  - `stated_keys` gains `via_ticker_id`, `via_market` and `via_venue`, with the
    composite foreign keys of
    [008](../adr/008-a-ticker-assertion-records-the-venue-and-currency-its-source-stated.md).
    `via_type`, `via_domain` and `via_value` go. `via_id` becomes `via_identifier_id`
    ([013](../adr/013-a-users-choice-fixes-a-key-through-a-stable-identifier.md)).
- The get-or-create function, which replaces `spelled` and `check` in
  `validity/record.go`.
- `writer.go` and `recordUnknown` in `write.go` assert only venue tickers.
- `validity/store.go`, `ListRunAssertions`, `ListTickersNaming` and the `corpact`
  queries read by ticker id.
- Comments: the assertion, identifier event, coverage and `market_members` comments in
  `010_validity.sql`, the comments in `011_corp_acts.sql`, and the `via_*` text in
  `004_statements.sql`.

Out:

- Edges, venue and currency on assertions, and the projection.

## Tests

- The validity, resolve, corpact and replay dbtests pass on the new schema.
- Get-or-create returns one id for one ticker, and refuses a market outside `markets`.
- `cascade_test.go` shows that a ticker row outlives the fetch key of its facts.
- A db dbtest shows that the composite foreign keys refuse a venue in another market.
