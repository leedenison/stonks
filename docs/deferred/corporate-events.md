---
title: Corporate events
recorded: 2026-08-30
---

# Corporate events

The splits and similar events that restate the quantity of an instrument held, so
transactions recorded under different conventions can be expressed in the same one.

The mergers, retirements, reassignments and similar events that limit the validity
of instrument identifiers, so resolution of instruments can be carried out.

## Why

Any source of, for example, transaction or price data is, often implicitly, split adjusted
as at a particular date.  In order to be able to accumulate like for like holdings across
transactions we must be able to adjust quantities and prices forward to the present day. We
need an up to date and accurate corporate event calendar to be able to make these
adjustments.

Some security identifiers (eg. ticker symbols) are retired, merged or reassigned in
corporate events.  We need an up to date corporate events calendar to be able to identify
securities via these kinds of identifier.

Some derivative identifiers (eg. OCC identifiers) incorporate strike price in the identifier
which must also be adjusted for stock splits.  This means we need an up to date corporate
events calendar to be able to identify OCC identified options.

## Constraints

### Unhandled Events

Corporate events can be extremely complex to handle.  For example, non-whole numbered
reverse splits applied to OCCs can be tricky to implement correctly.  The more esoteric an
event less common that it is which makes it difficult to see examples of how brokers or
market datasources handle these kinds of events.  They also represent diminishing returns
for the complexity of implementing relative to the likelihood of occurring.  The system
will implement the option to simply store an event as unhandled so that the administrator
is informed and can decide what to do about it, without the need to implement the most
complex handling up front.

An unhandled event is a row. The admin interface queries those rows for the current backlog
and drives its alert from them, and the row's own timestamps carry the trend, so nothing is
copied elsewhere to report on it. Telemetry carries at most a bounded mirror for the
operational dashboard: events met by kind per run, and a sampled count of the backlog. The
alert is not driven from that mirror, which is batched, expired and absent whenever no
collector is configured.

## Shared Datasource Constraints

### On-Demand Fetch

We generally have no datasource that offers bulk fetching of corporate event data.  We
therefore need to fetch events for the instruments held by users on demand covering
the period the instrument was held.  The datamodel must be able to express which
instruments we have successfully fetched corporate event data for, what time periods they
cover and whether retrieval of a particular instrument from a particular datasource
failed temporarily or permanently.

### Fetching from Multiple Sources

We generally have no datasource that offers corporate event data for the complete range
of instruments we want to accommodate (due to coverage limits on geography, asset class,
historic period, etc per datasource).  The system must fetch corporate events from an
ensemble of datasources.

### Minimizing Fetch Cost

Fetching data from a datasource is generally expensive due to API quota limits, rate
limits, etc.  The system should avoid fetching duplicate data when more than one
source covers corporate events for the same instrument.  The system should also avoid
making API calls which are guaranteed to fail because the datasource is known not to
provide the corporate events requested.

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
system must tolerate a temporary or permanent absence of corporate event data for any
given instrument by:

- Making assumptions where the retroactive addition of an unknown corporate event can be
  idempotently applied correctly.
- Storing limited, conservative data.  For example, storing a transaction quantity raw
  with no split adjusted quantity while we have no split data coverage for the instrument.
- Refusing data with an error when the retroactive addition of an unknown corporate event
  would leave some data incorrect and unrecoverable.

## Invariants

### Maintain Lookahead

Where a datasource can return corporate event data for an instrument the system maintains
a lookahead as far into the future as the datasource can provide.  Where a provider
disallows open ended requests then a lookahead window is configured for the datasource.

### Adjusted Prices and Quantities are Current

Adjusted prices and quantities of instruments always return the current correct value
based on all known splits and dividends for the instrument, or its underlying in the case
of derivatives.  This is straightforwardly true of any adjusted values computed on demand,
but the system also ensures that any cached, adjusted values affected by a corporate event
are invalidated and recomputed.

### Instrument Identifiers Validities are Current

Instrument identifiers always return validity ranges that a consistent with all mergers,
retirements, reassignments, etc due to known corporate events.  Any cached validity ranges
affected by a corporate event are invalidated and recomputed.

## Sketch

Corporate events are system owned data and can only be modified by an admin.

An event per instrument, with an event type and ex-date, carrying the ratio as a
from and a to rather than as a single factor so that an exact rational survives. Restating a
value multiplies by the product of the ratios of the events between the date it is stated as
at and the target date, computed exactly and rounded once for display.

## Undecided

- Is there a primary key given that many events could occur on the same day?
- Events that change which instrument is held (eg. mergers, spinoffs, and identifier
  changes). How do we represent these?  What information is needed to allow these to be
  applied retroactively?
- How do we get a (mostly) complete list of event types?  Even for unhandled events we
  would like to store the information we _would_ need to handle the event correctly in
  future.
