---
title: EODHD prices and rates
type: task
---

## Scope

The EODHD client serves end of day prices for a listing and end of day FX rates for a
currency, through the `price` package.

## Design

* The client takes the raw close of each daily bar and states it as at its own date. It
  never takes the adjusted close, which EODHD adjusts for dividends. See
  [003](../adr/003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).
* A daily bar omits the currency. The client takes the currency code from the metadata
  of the symbol and states it with each price. It states USD for a US symbol without
  reading the metadata. See
  [004](../adr/004-a-price-is-stored-in-the-family-currency-of-its-listing.md).
* A rate is the USD price of one unit of the currency. The client asks for the pair
  `{code}USD.FOREX`, which EODHD quotes in that direction, and does not invert it. A code
  that EODHD lacks against USD stays a gap. See
  [001](../adr/001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).
* The config of the datasource names the plan of the account: `free`, `all_world`,
  `all_world_extended`, `all_in_one` or `fundamentals`. The free plan serves one year,
  and its window starts on the UTC day of the fetch, less one year, plus one day. The
  client refuses a period that ends before the window, so the gap passes down the
  precedence without a call. The fundamentals plan does not serve prices. See
  [002](../adr/002-datasource-precedence-picks-the-price-for-each-day.md).
* Every request asks for the whole history of the symbol, without dates. EODHD returns
  the bars the plan serves, and the client keeps the bars inside the asked period.
* Coverage starts at the later of the window and the first bar served, and ends at the
  last bar served. See
  [011](../adr/011-price-coverage-ends-at-the-last-price-served.md).
* A symbol that EODHD does not know is an answer without coverage.

## Tests

* The client is tested against recorded traffic in the integration tier.

## Facts

Read from EODHD's API and documents on 2026-11-25.

* The free plan serves one year of daily bars and 20 calls a day, for prices and for
  rates. A price outside the US costs two calls, the metadata and the bars, so the plan
  answers ten such listings a day. A US price or a rate costs one call. When the
  allowance is spent, the datasource pauses until midnight UTC.
* `close` is as traded. `adjusted_close` is adjusted for splits and dividends. The
  `Currency` member of a search row states the currency of a symbol, and it states GBX
  for a London symbol priced in pence.
* The symbol of a pair is `{base}{quote}.FOREX`, and its close is the number of units
  of the quote that one unit of the base buys. EODHD lists `{code}USD` for EUR, GBP,
  KRW, ILS and ISK.
* A currency series ends in a bar for the current UTC day while that day runs. The
  `price` package leaves the day of an answer out of its coverage, so the bar is never
  stored.
* The daily bars endpoint answers an unknown symbol with a 404 and the body
  `Ticker Not Found.`.
