---
title: Corporate event framework
type: task
---

## Scope

The records that hold corporate events, and the fetch kind that writes them.

In:

- A corporate event fetch kind beside identity and identifier events, with a capability
  interface that a datasource implements.
- Corporate events and coverage keyed on ticker and market, each referencing the fetch
  key that wrote it. The latest fetch of a datasource for a ticker replaces its earlier
  answers. See [001](../adr/001-a-corporate-event-is-stored-against-its-ticker-and-attributed-through-validity.md).
- A corporate event carries its ex-date, its ratio as an exact from and to, and its
  type. Several corporate events may share a ticker and a day.
- Coverage ends on the day the datasource answered, which is `answered_at` for a cached
  answer.
- The `unhandled_corporate_event` finding, which references the stored row, and the
  coverage it removes. See [004](../adr/004-an-unhandled-corporate-event-ends-coverage-on-its-ex-date.md).
- The `corporate_event_disagreement` finding between two datasources that cover one
  ticker and period. The `disagreement` kind covers identifier facts.
- A read of the corporate events and coverage of a set of tickers over a period, for
  holdings and the stated split check.
- Reword the comment on `identifiers` in `003_instruments.sql` to tie the match of an
  option symbol to the normalisation of option symbols.

Out:

- Any provider. These are [004](004-massive-corporate-events.md) and
  [005](005-eodhd-corporate-events.md).
- What starts a fetch. This is [008](008-corporate-event-fetch-triggers.md).
