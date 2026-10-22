---
title: Identifier events
recorded: 2026-09-14
---

# Identifier events

The ticker changes, retirements and reassignments that bound the interval over which an
identifier names one instrument, and the assumptions the system makes where no source
reports them.

## Why

Some identifier types are reassigned routinely: when one company retires a ticker,
another reuses it.  When a transaction is stated under such an identifier, it names an
instrument only for the date it was stated, and when a price series is fetched under the
identifier, the series names an instrument only for the dates on which the identifier
meant what it meant at the fetch.

No source reports these events for every venue.  Where none does, the system proceeds on
the assumption that the identifier has not moved, records enough to find every row that
rests on the assumption, and unwinds those rows when an event shows it false.

## Model

### Events

An event is keyed on the natural key of the identifier it touches: type, domain and value.
It carries the period in which it took effect and what it did: retired the value,
assigned it, or renamed it to another value.  An event names no instrument primary key.

An event is witnessed, implied or inferred:

- A witnessed event is reported by an identifier event source and takes effect on one
  date.
- An implied event is written from a corporate event that retires a listing.  It takes
  effect on one date and counts as no coverage.
- An inferred event is derived when two assertions of one value name different
  instruments.  It takes effect somewhere between the two assertions.  It is never
  stored, since the assertions imply it.

Every stored event references the run that gave rise to it.

Events are stated against MIC_TICKER, with the operating MIC as domain.  Every
MIC-derived identifier moves when its MIC_TICKER does, so events and coverage for the
MIC_TICKER serve them all.

### Assertions

A fetch whose answer returns an identifier asserts that the identifier names the instrument
the answer described.  An identity lookup asserts, and so does a price or corporate event
fetch whose answer echoes the identifier it served.  An assertion holds on one day: the
day of the answer, or the date a point in time lookup requested.  An entity's ticker
history asserts each ticker on the day the ticker began naming the entity.  It covers the
ticker until the next event.

Assertions are rows of the fetch key that made them, never fields on the identifier.  An
administrator asserts in the same way where no datasource answers.  A user statement is
not an assertion.

### Coverage

Coverage follows the datasource framework, keyed on a value or on a domain.  Coverage of
a value over a period means every event touching that value in the period is known.
Coverage of a domain over a period means the same for every value in the domain.  Inside
coverage, the absence of an event for a value is a claim that none occurred.  Outside
coverage it is silence.

Where a provider answers for the entity a value names, it reports only that entity's
chain, so its answer covers each ticker in the chain over the interval the ticker named
the entity.  An earlier entity's retirement of the value is invisible from this answer.
The entity's acquisition of the value is an assigned event whichever entity came before.

Where a provider serves changes by date range, it covers the domain over the range.
Where the answer does not mention a value, the value gains a negative assertion through
the domain row and is never written individually.  A provider that names a US ticker at
composite level, where one symbol is unique across the consolidated tape, witnesses no
event when a listing moves between venues and keeps its symbol.  Its answer covers every
operating MIC in the composite, one row per MIC.

Coverage is written only from a fetch key with a served outcome.  A failed or truncated
fetch covers nothing, since a domain row from a partial answer would assert no event for
every value the missing part touched.

### Validity

Validity of a value to one instrument is derived from assertions, coverage and events.
It is derived when read.

The confirmed interval is the union of the asserted days, extended through covered
periods to the nearest event either side.  Two assertions naming the same instrument
bracket a confirmed interval between them.

Beyond the confirmed interval, validity extends provisionally until the nearest event.
Where a transaction's date lies in the provisional interval, it is associated on the
assumption that the value did not move.

Two assertions naming different instruments bound an inferred event.  The later
instrument's validity begins at its first assertion.  The earlier instrument stays
provisionally valid up to the later assertion.  A key dated between the two therefore
resolves the same way whenever it is ingested.

### Assumptions

- A value outside coverage has not moved.  A dated association rests on this assumption
  at each read, and the assumption is not stored.  A fetch made under it is recorded
  against the run that made it, so the assumption can be unwound.
- Where a value is asserted twice for one instrument, it was not reassigned away and back
  between the two assertions.  Should that happen unwitnessed, the system records
  incorrect information and accepts it.
- A witnessed event inside a bracket of same-instrument assertions breaks the bracket.
  The event wins, and the broken bracket is recorded as a finding of the run that wrote
  the event.  See [006_findings.sql](../../server/internal/migrations/006_findings.sql).

## Constraints

### Explicit Fetches Upgrade Validity

Identifier events are fetched before a batch is resolved, where a source serves the
domain or a stable identifier concerned, covering the earliest transaction date to the
present.  The fetch converts provisional validity to confirmed.  Its absence blocks
nothing: resolution proceeds on provisional validity, and a dated association reads
coverage or an event when it arrives.

A range provider is fetched once per domain and period.  An entity provider is fetched
once per stable identifier.

### Domain fetches

The period of a domain fetch follows the rule on `NextPeriod` in
[identifier_events.go](../../server/internal/market/identifier_events.go).  A range
provider implements `market.Ranged`, and `datasources.lag_days` overrides its lag.

The lag makes each fetch repeat the most recent part of the previous period.  An event
that a provider publishes late is still recorded, and it unwinds what rested on the
claim that no event occurred.  Coverage that ended a lag before the present would leave
every recent transaction outside coverage.

An administrator starts a domain fetch from the admin area.  The datasources page shows
the lag of each datasource and lets an administrator change it.

### Unwinding

An event on a value finds every fetch key that sent the value to a provider.  A data row
produced by such a fetch key is invalidated when the event lies between the row's date
and the fetch's assertion moment.  Invalidated rows and the coverage written from their
fetch keys are dropped and refetched.  A transaction of a dated association reads
the instrument the value names on its date.

No instrument is merged through a MIC-derived identifier, so no event unwinds a merge.
See [instrument-resolution.md](instrument-resolution.md).

### Datasources

The datasource framework constraints apply.  An absence of events for an identifier is
tolerated by provisional validity, and a dated association reads coverage or an event
that arrives later.  An unresolved key that states
the value is replayed when a fact for the value arrives.

Where a ticker is stated without its venue, it has no natural key, so no fetch is made
for it and no coverage arrives for it.  Where a transaction states nothing else, its key
resolves through its broker description or stays unresolved until a user or administrator
supplies the venue.

## Invariants

### Identifier Validities are Current

Validity intervals are consistent with every known event, assertion and coverage row.

### Symbols are Normalised Before Comparison

An integration maps the provider's spelling of a symbol to the system's before an event
or an assertion is recorded, so when an event is reported under one spelling, it touches
the value stored under the other.  A missed match inside domain coverage turns a known
event into a claim that none occurred.

## Sketch

Two integrations of opposite shape prove the framework interface: Massive, which answers
for the entity a symbol names and accepts a date to answer as at, and EODHD, which
serves symbol changes by date range for the US composite.

EODHD's range endpoint is `GET /api/symbol-change-history?from=&to=&fmt=json`:

- One request costs one call.  `fmt` defaults to CSV.
- Each row states `exchange`, `old_symbol`, `new_symbol`, `company_name` and
  `effective`.  The rows are renames only, and `exchange` is always `US`.
- Data starts on 2022-07-22.
- Only the All World Extended and All-In-One plans include it.
- The documentation says the data is updated daily.  A support reply on EODHD's forum
  says a change usually appears within one to two weeks.  The default lag of two weeks
  rests on that reply.
- `GET /api/exchange-symbol-list/US?delisted=1` lists delisted symbols without dates.
- Unverified: the share class spelling in `old_symbol` and `new_symbol`, the status a
  plan without the endpoint receives, and whether a `from` before 2022-07-22 is
  refused.  A plan without the endpoint can record the second.

The endpoint adds what identity lookups cannot.  A witnessed rename places a change on
one day.  Domain coverage confirms a present assertion back to the last rename without
an attestation.

The integration is tested as follows:

- A refusal because the plan lacks the endpoint blocks the datasource for the
  identifier events kind.
- A refusal of a `from` before the first date fails only the request.
- Tests run against recorded traffic.
- End to end coverage runs through a range that holds a rename.

Holdings and valuation read a per instrument summary of validity and coverage maintained
at ingest rather than the fetch tables.  See [datasources.md](datasources.md).

## Undecided

- How often domain coverage is extended to the present.

- Whether two providers asserting different instruments for one value at the same moment
  is resolved by precedence or left as a gap.

- Whether a holding whose identity rests on provisional validity is shown as such.
