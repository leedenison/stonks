---
title: Display currency
type: task
dependencies: [008, 010]
---

## Scope

Each user has a display currency, selects it on the profile page, and starts a rate fetch
by the selection. See
[008](../adr/008-end-of-day-prices-feed-charts-and-the-display-currency-has-no-effect-yet.md).

The display currency affects only the profile page in this milestone.

## Design

* The user row carries a display currency that defaults to USD. It references a family
  currency.
* The profile page offers the family currencies. It leaves out a scaled code such as GBX.
* A selection puts the currency in use. See
  [005](../adr/005-the-days-held-are-demanded.md). When the currency is new to the
  system, the selection starts a rate fetch for it.

## Tests

* An e2e spec selects a currency in which the holdings of the fixtures are not priced. It
  then reads the fetched rates from the rate chart on the details page of that currency.
