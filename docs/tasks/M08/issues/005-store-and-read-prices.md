---
title: Store and read prices
type: task
---

## Scope

The price framework that each datasource client and each reader builds on: the table,
its coverage, the gaps a fetch asks for, and the read. It covers prices and FX rates
alike. See [001](../adr/001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md).

The datasource clients, the runs, the Google Sheets tool and the chart each have an
issue of their own.

## Design

Storage:

* A price row states its listing, its day, its datasource, its price in the family
  currency of the listing, its "as at" date and its fetch key.
* Each datasource records the days it covered for each listing, whether or not its
  answer had a price for them. A failed fetch records nothing.
* A client states the currency code of each price. The write checks the code against the
  family of the listing and applies its scale. See
  [004](../adr/004-a-price-is-stored-in-the-family-currency-of-its-listing.md).
* The USD listing of a currency is created when the currency comes into use.

Gaps:

* The demand follows [005](../adr/005-a-fetch-covers-the-days-held.md): the days held
  with a lead of one week for a price, and one span for every currency in use for a
  rate.
* The last completed day of a listing is the UTC day before today. See
  [009](../adr/009-a-ticker-answer-is-dated-by-its-utc-day.md).
* A gap for a datasource is a demanded day that neither it nor a datasource above it
  covers. See
  [002](../adr/002-datasource-precedence-picks-the-price-for-each-day.md).

Read:

* A read takes the row of the highest ranked datasource for each day.
* It restates each price to today through the corporate actions calendar and marks it
  provisional where the calendar does not reach today. See
  [003](../adr/003-a-price-is-stored-as-its-source-served-it-and-restated-on-read.md).
* A day that is covered and has no price takes the latest earlier close, provided the
  coverage between the two is unbroken.

## Tests

* Integration tests prove the precedence pick, the gap for each rank, the scale on
  write, the refusal of a code outside the family, and the carry across a day without a
  session.
* Unit tests prove the restatement of a mixed series across a split, and the
  provisional mark.

## Open at planning

* How a stated key reaches the listing it names, which decides the listings a holding
  demands.
