---
title: Price fetch runs
type: task
---

## Scope

The runs that fetch prices and rates, and what starts each. See
[006](../adr/006-a-fetch-asks-only-for-gaps-and-a-failed-fetch-leaves-one.md).

* An administrator starts a system run from the system runs panel. It asks for every
  gap in prices and in rates.
* An ingest starts a fetch for every gap in the prices of the listings that its user
  holds, and for every gap in the rates.

The display currency trigger belongs to [011](011-display-currency.md).

## Design

* A fetch stores every price the datasource serves. See
  [012](../adr/012-a-fetch-stores-every-price-the-datasource-serves.md).

* The fetch after an ingest follows the resolve run of that ingest and the corporate
  actions fetch for the same tickers.
* A run asks the datasources in precedence order. Each is asked for the gaps that the
  datasources above it left, including a gap that a failure left.
* A failure is per key. The run records its failed keys and stores the prices of every
  other key.

## Tests

* An e2e spec uploads a statement. It reads the fetches of the run that follows, and it
  reads the gaps that remain. Issue [010](010-price-chart.md) reads the prices.
* An integration test proves that a key that fails at the first datasource is asked of
  the second, and that the gap remains when both fail.
* The e2e proxy replays recorded prices and rates from Massive and from EODHD for the
  instruments of the e2e fixtures. The e2e Massive row names the plans `advanced` for
  stocks and `starter` for currencies. Each serves all history, so its window starts on a
  fixed day and a replayed answer keeps its coverage as the clock moves on. The e2e
  EODHD row names the plan `all_in_one` for the same reason.
