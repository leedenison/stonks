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
* A rate is the USD price of one unit of the currency. The client inverts a pair that
  Massive quotes the other way. See
  [001](../adr/001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).
* The client declares the first day its plan answers for, for prices and for rates. The
  client leaves each earlier day uncovered, so the gap passes down the precedence. See
  [002](../adr/002-datasource-precedence-picks-the-price-for-each-day.md).

## Tests

* The client is tested against recorded traffic in the integration tier.
* The e2e proxy replays recorded prices and rates for the instruments of the e2e
  fixtures.

## Facts to verify

* The history that the Basic plan serves, for stock bars and for currency bars.
* The flag that selects as traded bars, and that such a bar is free of any dividend
  adjustment.
* The symbol form and the quote direction of a currency pair.
