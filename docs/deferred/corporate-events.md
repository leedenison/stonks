---
title: Corporate events
recorded: 2026-08-30
---

# Corporate events

The splits and similar events that restate the quantity of an instrument held, so
transactions recorded under different conventions can be expressed in the same one.

## Why

Any source of, for example, transaction or price data is, often implicitly, split adjusted
as at a particular date.  In order to be able to accumulate like for like holdings across
transactions we must be able to adjust quantities and prices forward to the present day. We
need an up to date and accurate corporate event calendar to be able to make these
adjustments.

Some derivative identifiers (eg. OCC identifiers) incorporate strike price in the identifier
which must also be adjusted for stock splits.  This means we need an up to date corporate
events calendar to be able to identify OCC identified options.

## Model

An event names the instrument it applies to by primary key, so events are fetched only
once the instrument is resolved, covering the period it was held.

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

### Datasources

The datasource framework constraints apply, keyed on the instrument primary key.  An
absence of corporate event data for an instrument is tolerated by:

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

## Sketch

Corporate events are system owned data and can only be modified by an admin.

An event per instrument, with an event type and ex-date, carrying the ratio as a
from and a to rather than as a single factor so that an exact rational survives. Restating a
value multiplies by the product of the ratios of the events between the date it is stated as
at and the target date, computed exactly and rounded once for display.

## Undecided

- Is there a primary key given that many events could occur on the same day?
- Events that change which instrument is held (eg. mergers and spinoffs). How do we
  represent these?  What information is needed to allow these to be applied
  retroactively?
- Whether an event that retires an identifier, such as a merger, closes that identifier's
  validity interval.  It never counts as identifier event coverage.
- How do we get a (mostly) complete list of event types?  Even for unhandled events we
  would like to store the information we _would_ need to handle the event correctly in
  future.
