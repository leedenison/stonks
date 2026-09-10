---
title: Identifier events
recorded: 2026-09-10
---

# Identifier events

The ticker changes, retirements and reassignments that bound the interval over which an
identifier names one instrument.

## Why

Some identifier types are reassigned routinely: a ticker retired by one company is reused
by another.  A transaction stated under such an identifier names an instrument only for
the date it was stated, and resolution can use the identifier only when the events
touching it over the interval it is used in are known.

Providers serve this history separately from splits and dividends, keyed by identifier.

## Model

### Events

An event is keyed on the natural key of the identifier it touches: type, domain and value.
It carries the date it took effect and what it did: retired the value, assigned it, or
renamed it to another value.  An event names no instrument primary key, since it is
fetched before the instrument is resolved.

### Coverage

Coverage follows the datasource framework, keyed on the identifier natural key.  Coverage
of a value over a period means every event touching that value in the period is known.

A provider that answers for the entity currently holding a value reports only that
entity's chain.  A prior holder's retirement of the value is invisible until that holder
is asked about, so such an answer covers the value only from the date the current holder
acquired it.  A provider that serves changes by date range covers every value over the
range.

### Validity

Coverage bounds validity.  A claim that an identifier names an instrument is asserted at
a moment: the fetch time for a datasource answer, or the transaction as at date for a user
statement.  Coverage transfers to the claim only when that moment lies inside a covered
period.  The claim's validity is then the covered interval around that moment, ending at
the nearest event either side.  A datasource asked today answers about today, so coverage
of a value must reach the present before a datasource claim gains any validity.

The moment of assertion is consumed when validity is first computed.  Every date in a
validity interval is a date the claim held, so when coverage is extended the stored
interval is the anchor: an event inside the extension truncates it, and an extension with
no event lengthens it.

## Constraints

### Fetched Before Resolution

Identifier events for the routinely reassigned identifiers a batch states are fetched
before the batch is resolved, covering the earliest transaction date to the present.

### Datasources

The datasource framework constraints apply.  An absence of events for an identifier is
tolerated by:

- Leaving a routinely reassigned identifier unusable, so a transaction stating only such
  identifiers resolves to a broker description instrument until coverage arrives.
- Retaining the stated key with the transaction, so resolution is replayed when coverage
  arrives.

## Invariants

### Identifier Validities are Current

Identifier validity intervals are consistent with every known identifier event.  Any
cached validity interval affected by an event is invalidated and recomputed.

## Sketch

Nothing settled beyond the model.

## Undecided

- Whether a corporate event fetched after resolution, such as a merger that retires an
  identifier, may close a validity interval.  It never counts as coverage.

- OCC, OPRA and FUT_OPT symbols are rewritten by splits on the underlying, so the coverage
  they need is corporate event coverage of the underlying, which exists only once the
  underlying is resolved.  Resolving such an instrument is two stages, and whether these
  types are governed by this note or by corporate events is open.

- How an entity-centric provider's answer is turned into coverage of a value, and whether
  a prior holder is ever asked about.

- Whether a fetch is per value or per batch of values, given providers that serve changes
  by date range.
