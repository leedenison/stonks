---
title: Corporate actions
recorded: 2026-09-14
---

# Corporate actions

Corporate actions that retire a listing or change the instrument held, identifier
succession, and the restatement of prices and OCC symbols.

## Why

Any source of, for example, transaction or price data is, often implicitly, split adjusted
as at a particular date.  In order to be able to accumulate like for like holdings across
transactions we must be able to adjust quantities and prices forward to the present day. We
need an up to date and accurate corporate action calendar to be able to make these
adjustments.

Some derivative identifiers (eg. OCC identifiers) incorporate strike price in the identifier
which must also be adjusted for stock splits.  This means we need an up to date corporate
actions calendar to be able to identify OCC identified options.

## Model

When a corporate action retires a listing, it writes an assertion that the listing's
ticker names nothing from the ex-date, with an edge at the start of the ex-date. The
corporate action answer writes the assertion alone.

Corporate actions on an underlying decide how an OCC symbol is normalised, so an answer
that changes them replays every OCC stated key normalised through them.  See
[instrument-resolution.md](instrument-resolution.md).

### Identifier succession

A corporate action can issue an instrument a new stable identifier. A US reverse split
issues a new CUSIP, and so a new ISIN. A UK reorganisation issues a new SEDOL and ISIN.
The successor names the same instrument, which makes it a succession rather than a
reassignment. An instrument carries each of its stable identifiers with the day it took
effect, and the corporate action that issues one joins the old identifier and the new
under one instrument. Without that action, a datasource that answers with the successor
names what reads as a second instrument.

## Constraints

### Datasources

The datasource framework constraints apply.

## Invariants

### Adjusted Prices and Quantities are Current

Adjusted prices and quantities of instruments always return the current correct value
based on all known splits and dividends for the instrument, or its underlying in the case
of derivatives.  This is straightforwardly true of any adjusted values computed on demand,
but when a corporate action affects cached, adjusted values, the system also invalidates
and recomputes them.

## Sketch

Corporate actions are system owned data and can only be modified by an admin.

## Undecided

- Corporate actions that change which instrument is held (eg. mergers and spinoffs). How
  do we represent these?  What information is needed to allow these to be applied
  retroactively?
- How do we get a (mostly) complete list of corporate action types?  Even for unhandled
  corporate actions we would like to store the information we _would_ need to handle the
  action correctly in future.
- Whether an identifier is exclusive on an instrument for all time or on each day, once an
  instrument carries a succession of them.
