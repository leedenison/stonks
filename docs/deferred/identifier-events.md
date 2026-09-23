---
title: Identifier events
recorded: 2026-09-14
---

# Identifier events

The ticker changes, retirements and reassignments that bound the interval over which an
identifier names one instrument, and the assumptions the system makes where no source
reports them.

## Why

Some identifier types are reassigned routinely: a ticker retired by one company is reused
by another.  A transaction stated under such an identifier names an instrument only for
the date it was stated, and a price series fetched under it names an instrument only for
the dates on which the identifier meant what it meant at the fetch.

No source reports these events for every venue.  Where none does, the system proceeds on
the assumption that the identifier has not moved, records enough to find every row that
rests on the assumption, and unwinds those rows when an event shows it false.

## Model

### Events

An event is keyed on the natural key of the identifier it touches: type, domain and value.
It carries the period it took effect in and what it did: retired the value, assigned it,
or renamed it to another value.  An event names no instrument primary key.

An event is witnessed, implied or inferred:

- A witnessed event is reported by an identifier event source and takes effect on one
  date.
- An implied event is written from a corporate event that retires a listing.  It takes
  effect on one date and counts as no coverage.
- An inferred event is written when two assertions of one value name different holders.
  It takes effect somewhere between the two assertions, and that interval is what is
  recorded.

Every event references the run that gave rise to it.

Events are stated against MIC_TICKER, with the operating MIC as domain.  Every
MIC-derived identifier moves when its MIC_TICKER does, so events and coverage for the
MIC_TICKER serve them all.

### Assertions

A fetch whose answer returns an identifier asserts that the identifier names the instrument
the answer described.  An identity lookup asserts, and so does a price or corporate event
fetch whose answer echoes the identifier it served.  An assertion holds at the moment of
the fetch unless the answer states an interval: a point in time lookup asserts on the
date asked about, and an entity's ticker history asserts each ticker over the interval
the entity held it.

Assertions are rows of the fetch key that made them, never fields on the identifier.  A
user statement is not an assertion.

### Coverage

Coverage follows the datasource framework, keyed on a value or on a domain.  Coverage of
a value over a period means every event touching that value in the period is known.
Coverage of a domain over a period means the same for every value in the domain.  Inside
coverage, the absence of an event for a value is a claim that none occurred.  Outside
coverage it is silence.

A provider that answers for the entity holding a value reports only that entity's chain,
so its answer covers each ticker in the chain over the interval the entity held it.  A
prior holder's retirement of the value is invisible from this answer.  The entity's
acquisition of the value is an assigned event whichever holder came before.

A provider that serves changes by date range covers the domain over the range.  Values
the answer does not mention gain a negative assertion through the domain row and are
never written individually.  A provider that names a US ticker at composite level, where
one symbol is unique across the consolidated tape, witnesses no event when a listing
moves between venues and keeps its symbol.  Its answer covers every operating MIC in the
composite, one row per MIC.

Coverage is written only from a fetch key with a served outcome.  A failed or truncated
fetch covers nothing, since a domain row from a partial answer would assert no event for
every value the missing part touched.

### Validity

Validity of a value to one instrument is derived from assertions, coverage and events.
It is cached, never recorded as a fact of its own.

The confirmed interval is the union of the asserted intervals, extended through covered
periods to the nearest event either side.  Two assertions naming the same holder bracket
a confirmed interval between them.

Beyond the confirmed interval, validity extends provisionally until the nearest event.
A transaction whose date lies in the provisional interval is associated on the
assumption that the value did not move.

Two assertions naming different holders bound an inferred event.  The earlier holder's
validity ends at its last assertion, the later holder's begins at its first, and between
them the value names nothing.  A transaction dated in that gap resolves to its broker
description.

### Assumptions

- A value outside coverage has not moved.  Every association and every fetch made under
  this assumption is recorded against the run that made it, so the assumption can be
  unwound.
- A value asserted twice for one holder was not reassigned away and back between the two
  assertions.  Should that happen unwitnessed, the system records incorrect information
  and accepts it.
- A witnessed event inside a bracket of same-holder assertions is a contradiction.  The
  event wins, and the contradiction is recorded as a finding of the run that met it.  See
  [runs.md](runs.md).

## Constraints

### Explicit Fetches Upgrade Validity

Identifier events are fetched before a batch is resolved, where a source serves the
domain or a stable identifier concerned, covering the earliest transaction date to the
present.  The fetch converts provisional validity to confirmed.  Its absence blocks
nothing: resolution proceeds on provisional validity and is replayed when coverage or an
event arrives.

A range provider is fetched once per domain and period.  An entity provider is fetched
once per stable identifier.

### Unwinding

An event on a value finds every fetch key that sent the value to a provider.  A data row
produced by such a fetch key is invalidated when the event lies between the row's date
and the fetch's assertion moment.  Invalidated rows and the coverage written from their
fetch keys are dropped and refetched.  A transaction whose date falls outside the
validity it was associated under is replayed from its stated key.

No instrument is merged through a MIC-derived identifier, so no event unwinds a merge.
See [instrument-resolution.md](instrument-resolution.md).

### Datasources

The datasource framework constraints apply.  An absence of events for an identifier is
tolerated by provisional validity and by retaining the stated key with the transaction,
so resolution is replayed when coverage or an event arrives.

A ticker stated without its venue has no natural key, so it is never fetched for and no
coverage arrives for it.  A transaction stating nothing else stays on its broker
description instrument until a user or administrator supplies the venue.

## Invariants

### Identifier Validities are Current

Validity intervals are consistent with every known event, assertion and coverage row.
Any cached validity affected by a new row is invalidated and recomputed.

### Symbols are Normalised Before Comparison

An integration maps the provider's spelling of a symbol to the system's before an event
or an assertion is recorded, so an event reported under one spelling touches the value
stored under the other.  A missed match inside domain coverage turns a known event into
a claim that none occurred.

## Sketch

Two integrations of opposite shape prove the framework interface: Massive, which answers
for the entity holding a symbol and accepts a date to answer as at, and EODHD, which
serves symbol changes by date range for the US composite.

Holdings and valuation read a per instrument summary of validity and coverage maintained
at ingest rather than the fetch tables.  See [datasources.md](datasources.md).

## Undecided

- What period a domain fetch requests, and how often domain coverage is extended to the
  present.

- Whether two providers asserting different holders for one value at the same moment is
  resolved by precedence or left as a gap.

- Whether a holding whose identity rests on provisional validity is shown as such.
