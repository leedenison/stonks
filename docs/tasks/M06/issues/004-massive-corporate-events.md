---
title: Massive corporate events
type: task
dependencies: [003]
---

## Scope

Massive serves corporate events through `GET /stocks/v1/splits`, keyed by ticker. Each
corporate event states `execution_date`, `split_from`, `split_to`, `adjustment_type` and
an `id`. The documented types are `forward_split`, `reverse_split` and `stock_dividend`.

## Open questions

Settle each by recording before the plan is written.

- How far back Basic answers. The documentation states two years, and other Massive
  endpoints answer further back on Basic.
- Whether the answer for a renamed ticker includes the splits from before the rename.
- The complete set of `adjustment_type` values.
- Which errors are temporary, and which tickers Massive does not serve. The 2026-02-18
  release note states that the endpoint drops unsupported tickers such as mutual funds.

## Design

- Parse each ratio from the JSON number as a decimal string.
- Splits are announced weeks before their ex-date, so a fetch asks past today and
  stores future corporate events.
- A request whose query states today's date must replay from a recording on any later
  day. The recording proxy and `testutil/vcr` match a request on its method, URL and
  body. They gain a match with the dates in the query normalised, and their package
  comments say so.
