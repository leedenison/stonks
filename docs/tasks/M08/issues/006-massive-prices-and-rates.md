---
title: Massive prices and rates
type: task
---

## Scope

The Massive client serves end of day prices for a US listing and end of day FX rates for
a currency, through the `price` package.

## Design

* The client asks for as traded daily bars and states each price as at its own date. See
  [003](../adr/003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).
* Massive serves US listings alone, so the client states USD as the currency of every
  price.
* A rate is the USD price of one unit of the currency. Massive lists every pair in both
  directions, so the client asks for the pair that is quoted in USD.
  See [001](../adr/001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).
* The client declares the first day its plan answers for, for prices and for rates. The
  client leaves each earlier day uncovered, so the gap passes down the precedence. See
  [002](../adr/002-datasource-precedence-picks-the-price-for-each-day.md).
* Coverage ends at the last bar served. See
  [011](../adr/011-price-coverage-ends-at-the-last-price-served.md).
* Every request asks for the same fixed range of dates, and Massive returns the bars that
  the plan serves. A recording of the request therefore replays on any later day.
* The config names the stocks plan and the currencies plan, because Massive sells each
  product apart.

## Tests

* The client is tested against recorded traffic in the integration tier.

## Facts

Each fact was read from the API on 2026-11-25 with a Basic key, unless it names the
documentation.

* Basic serves two years of stock bars and of currency bars. The documentation gives
  Stocks Starter 5 years, Developer 10 and Advanced all history from 2003-09-10. It gives
  Currencies Starter all history from 2009-09-25.
* A request that reaches back past the history returns the bars that the plan serves. A
  request that lies wholly before the history is refused with a 403.
* `adjusted=false` selects as traded bars. A bar is free of any dividend adjustment:
  the closes of a ticker that paid dividends and did not split are the same with either
  setting.
* The close of a daily stock bar is the close of the regular session.
* A pair is `C:` then the two codes, and the close of `C:EURUSD` is the number of USD that
  one EUR buys.
* An unknown ticker and a known ticker without a session both return an empty list.
* Massive counts requests for each product apart: five a minute on Basic.
