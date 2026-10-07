---
title: Price ingestion
recorded: 2026-09-14
---

# Price ingestion

Fetching of historic end of day prices from external sources for the instruments held.
Fetching of FX end of day rates from external sources for conversion from the price
currency to a user specified base currency.

## Why

Instruments must be priced in order to enable valuation of holdings.

## Constraints

### Minimized Currency Conversions

To avoid ultimately storing N^2 FX pairs we perform conversions via a system wide base
currency of USD.

### As At Convention

A fetched price must carry an "as at" date which declares the date at which the value was
true.  This tells the server the corporate events for which the price is adjusted.  Where a
datasource restates its history as corporate events arise, it answers differently for the
same trading day either side of an ex. date. Where a price is stored without an "as at"
date, it can neither be compared with nor accumulated alongside one fetched at another
time.

A typical convention for a datasource serving adjusted prices is that the whole series is
stated as at the date of the request, and for one serving as traded prices that each price
is stated as at its own date.  However, only the client for a particular datasource can
know its conventions.  So the client must interpret the conventions and provide an explicit
"as at" date to the server.

An FX rate is not restated by corporate events, so it is stated as at its own date.

The "as at" date says nothing about the identifier sent to fetch the price.  That
identifier and the moment it was asserted are carried by the fetch key the price
references.  See [datasources.md](datasources.md).  The two dates coincide for an
adjusted series and differ for an as-traded one, and neither convention is refused.

### Venue

Some price sources, such as GOOGLEFINANCE, take a ticker only together with its venue,
since one symbol can name different securities on different venues. Such a source is
asked through one of the MIC_TICKERs a listing carries.

### Provenance

When a price dated d is fetched at t under a MIC-derived identifier, it rests on the
assumption that the identifier named the same instrument on d as at t.  An identifier
event between d and t invalidates the price, whatever its "as at" date, and the price is
refetched.  A fetch is keyed on a stable identifier where the provider accepts one, which
yields a fresh assertion of the provider's ticker for it, and the fetch key records that
ticker as the identifier sent.

### Datasources

The datasource framework constraints apply, keyed on the instrument primary key.  An
absence of price data for an instrument is tolerated by:

- Accommodating periods of unknown price data in the user interface by expressing the
  limits of our knowledge.

## Sketch

Nothing settled. It needs a provider, a way to map an instrument to whatever the provider
calls it, a schedule, and a story for the provider being down or wrong.

Testing it needs two different things at two tiers. The client that parses what the provider
sends is tested against recorded traffic, which is what the integration tier already does.
The e2e suite drives the shipped binary, so its provider is a proxy in the e2e overlay that
replays traffic recorded from the real one; a price provider would be a second upstream of
that proxy, or a second instance of it.

## Undecided

Which provider, and whether more than one. Whether a fetch failure leaves the last known
price in place or marks the valuation stale.
