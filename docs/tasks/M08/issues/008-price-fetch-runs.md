---
title: Price fetch runs
type: task
dependencies: [007]
---

## Scope

The runs that fetch prices and rates, and what starts each. See
[006](../adr/006-a-fetch-asks-only-for-gaps-and-a-failed-fetch-leaves-one.md).

* An administrator starts a system run from the system runs panel. It asks for every
  gap in prices and in rates.
* An ingest that adds a holding starts a fetch for the prices of that holding and for
  the rates.

The display currency trigger belongs to [011](011-display-currency.md). The Google
Sheets tool belongs to [009](009-google-sheets-tool.md).

## Design

* The fetch after an ingest follows the resolve run of that ingest and the corporate
  actions fetch for the same tickers.
* A run asks the datasources in precedence order. Each is asked for the gaps that the
  datasources above it left, including a gap that a failure left.
* A failure is per key. The run records its failed keys and stores the prices of every
  other key.

## Tests

* An e2e spec uploads a statement and reads the fetched prices from the price chart of
  [010](010-price-chart.md).
* An integration test proves that a key that fails at the first datasource is asked of
  the second, and that the gap remains when both fail.
* The e2e proxy replays recorded prices and rates from Massive and from EODHD for the
  instruments of the e2e fixtures. The e2e Massive row names the plans `advanced` for
  stocks and `starter` for currencies. Each serves all history, so its window starts on a
  fixed day and a replayed answer keeps its coverage as the clock moves on. The e2e
  EODHD row names the plan `all_in_one` for the same reason.
