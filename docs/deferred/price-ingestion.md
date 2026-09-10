---
title: Price ingestion
recorded: 2026-08-29
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
true.  This allows the server to know which corporate events the price is adjusted for.  A
datasource that restates its history as corporate events arise answers differently for the
same trading day either side of an ex. date. A price held without an "as at" date can
neither be compared with nor accumulated alongside one fetched at another time.

A typical convention for a datasource serving adjusted prices is that the whole series is
stated as at the date of the request, and for one serving as traded prices that each price
is stated as at its own date.  However, only the client for a particular datasource can
know its conventions.  So the client must interpret the conventions and provide an explicit
"as at" date to the server.

An FX rate is not restated by corporate events, so it is stated as at its own date.

## Shared Datasource Constraints

### On-Demand Fetch

We generally have no datasource that offers bulk fetching of price data.  We
therefore need to fetch prices for the instruments held by users on demand
covering the period the instrument was held.  The datamodel must be able to express which
instruments we have successfully fetched prices for, what time periods they cover and
whether retrieval of a particular instrument from a particular datasource failed
temporarily or permanently.

### Fetching from Multiple Sources

We generally have no datasource that offers price data for the complete range of
instruments we want to price (due to coverage limits on geography, asset class, historic
period, etc per datasource).  The system must fetch prices from an ensemble of
datasources.

### Minimizing Fetch Cost

Fetching data from a datasource is generally expensive due to API quota limits, rate
limits, etc.  The system should avoid fetching duplicate data when more than one
source covers prices for the same instrument.  The system should also avoid making API
calls which are guaranteed to fail because the datasource is known not to provide the
prices requested.

### Pluggable Datasources

The system architecture should assume that more datasources may be added as the system
evolves.  So the code which integrates to any given datasource should conform to a well
defined interface with the option to extract a given integration into a separately
maintained library.

Any particular running instance of the system may have access to a different
combination of datasources, so it must be possible to enable or disable datasource
integrations. 

### Partial or Incomplete Data

The datasources available may not have complete coverage of data for all instruments in
users' portfolios.  Or likewise a given datasource may be temporarily unavailable.  The
system must tolerate a temporary or permanent absence of price data for any given
instrument by:

- Accommodating periods of unknown price data in the user interface by expressing the
  limits of our knowledge.

## Sketch

Nothing settled. It needs a provider, a way to map an instrument to whatever the provider
calls it, a schedule, and a story for the provider being down or wrong.

Testing it needs two different things at two tiers. The client that parses what the provider
sends is tested against recorded traffic, which is what the integration tier already does.
The e2e suite is a different problem: it needs the provider to answer deterministically, not
authentically, and it cannot load a recording because the server it drives is the shipped
binary.

The sketch for that is a stub service in the e2e overlay, reached by configuration, serving
a corpus **keyed on the request rather than put into a scenario by a control call**. The
stub does not know which test is running; it knows that a lookup for `ZZNF` is a 404 and one
for `ZZRL` is a 429. A spec picks its case by choosing what to ask about, which keeps specs
independent and the suite parallel. An unknown key returns an error naming the key, never a
plausible default. A case that is genuinely stateful -- fails once, then succeeds -- needs a
key only one spec uses.

## Undecided

Which provider, and whether more than one. Whether a fetch failure leaves the last known
price in place or marks the valuation stale -- the spec currently says there is no
staleness bound, and this is what would change that.

Whether the stub's fixtures are generated from the integration tier's recordings, so there
is one source of authentic bytes and the stub cannot drift into answering in a shape the
provider abandoned. Attractive, and unnecessary until the stub exists.
