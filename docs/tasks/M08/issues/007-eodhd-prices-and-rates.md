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
  of the symbol and states it with each price. See
  [004](../adr/004-a-price-is-stored-in-the-family-currency-of-its-listing.md).
* A rate is the USD price of one unit of the currency. The client inverts a pair that
  EODHD quotes the other way. See
  [001](../adr/001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).
* The client declares the first day its plan answers for, for prices and for rates. The
  client leaves each earlier day uncovered, so the gap passes down the precedence. See
  [002](../adr/002-datasource-precedence-picks-the-price-for-each-day.md).
* Coverage ends at the last bar served. See
  [011](../adr/011-price-coverage-ends-at-the-last-price-served.md).

## Tests

* The client is tested against recorded traffic in the integration tier.

## Facts to verify

* The history and the daily call allowance that the free plan serves, for prices and for
  rates. The allowance decides whether the metadata call for a currency is affordable.
* That the raw close is as traded, and which field states the currency of a symbol.
* The symbol form and the quote direction of a currency pair.
