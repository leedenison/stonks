---
title: Price chart on the instrument details page
type: task
---

## Scope

A Prices tab on the instrument details page of the admin area. It charts every price the
system has for each listing of the instrument. A currency is an instrument, so the same
tab on the details page of a currency charts its rate. See
[008](../adr/008-end-of-day-prices-feed-charts-and-the-display-currency-has-no-effect-yet.md).

A chart of the valuation of a holding is out of scope.

## Design

* An admin RPC returns the price series of each listing of an instrument, through
  `price.Read`. Each point states its day, its price restated to today, its datasource
  and whether it is provisional.
* The chart draws one line for each listing, in the family currency of the listing.
* The datasource of each point is visible, so an administrator can check that a series
  lines up where two sources meet.
* The line breaks where a day lacks a price. A provisional span is marked and stays
  drawn.
* A details page exists for a currency, reached as the page of any other instrument is.

## Tests

* A benchmark restates twenty years of daily prices for one listing across several
  splits. Planning sets its bound. See
  [003](../adr/003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).
* An e2e spec opens the tab for a security and for a currency and reads the series each
  shows.
* The e2e spec `fetch-prices.spec.ts` also reads the prices that its run fetched. The
  e2e recordings come from keys that serve one or two years, and the e2e datasources
  name plans that serve every year. A recorded answer therefore covers earlier days
  without a price, so the spec reads a day inside the recorded years.
