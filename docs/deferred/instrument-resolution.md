---
title: Instrument resolution
recorded: 2026-09-20
---

# Instrument resolution

How resolution handles the life of a listing, options, guesses and the validity of a
MIC-derived identifier, and the replays that keep an association current.

## Why

Brokers name instruments in their own terms.  A key states what one broker called a
line, so one security held at two brokers is two keys, and holdings, prices and events
cannot be aggregated across them until a datasource answers for the identifiers those
keys state.

## Model

### Listings

A listing carries its tradeable interval. A delisting closes one. A redenomination
closes one and merges its contents into the listing taking over.

A currency instrument's listing in another currency carries the rate between the two, and
is made when a rate is first fetched.

An option or a future references the listing of its underlying, a strike being quoted
in the listing's currency.

### Validity and the Weakest Link

A MIC-derived identifier is trusted inside its validity interval, which is confirmed
inside identifier event coverage and provisional outside it.  See
[identifier-events.md](identifier-events.md).  It associates a key with an instrument
when the key's transactions are dated inside its validity, but never decides between two
instruments and never merges them.

The weakest link governs.  Where a key states only a ticker, it is associated through
that ticker however many stable identifiers a datasource's answer names, so the
association is provisional until the ticker is covered.

### Options

An OCC symbol embeds the underlying's ticker as its root and the strike in current terms.
It is renamed when the underlying's ticker is, which is the MIC-derived path, and
rewritten by corporate events on the underlying, which is separate.  A rewrite reassigns
a symbol between contracts: after a split the contract that held a strike moves to
another symbol, and the symbol it left names a different contract.

Resolution of an OCC symbol is two stages.  The root resolves the underlying as a
MIC_TICKER.  The contract is then normalised through the underlying's corporate events
between the transaction date and the present to a canonical contract: underlying, expiry,
right, and strike and deliverable stated in current terms.  The symbol is admitted only
when corporate event coverage of the underlying spans that interval.  Otherwise it stays
in the stated key and is replayed when coverage arrives.  Corporate events on the
underlying therefore create no assumption and nothing to unwind.

### Authority

Instruments, listings and identifiers are system owned and written only from a source
with system authority.  A source with user authority, a statement or an annotation,
writes keys and what the user records against them, and never instrument data.  A
source with candidate authority, any guess, writes nothing: a guess ranks candidates and
never filters them.

## Constraints

### Merger of Instruments

Two datasource answers, or an answer and the database, may describe one instrument.  They
merge only when they share a stable identifier.  Where answers overlap only on a
MIC-derived identifier, they are not merged: precedence picks the instrument kept, the
identifiers of the other are dropped, and the outcome is recorded as a finding of the
run.

### Re-Resolution

Datasources gain coverage, integrations are enabled and quota tiers change.  A
scheduled replay therefore re-attempts unresolved keys.

When an identifier event leaves a key's transactions dated outside the validity of the
key's association, the event replays the key.  Holdings, event grouping and cached
adjusted values derived from the key's transactions are recomputed.

## Invariants

### An Identifier Names One Instrument At A Time

No two instruments are identified by one identifier triple over overlapping validity
intervals.

### No Merge Through a MIC-derived Identifier

Instruments merge only through a stable identifier, so no identifier event unwinds a
merge.

## Sketch

Identifier events for the MIC-derived identifiers a batch states are fetched first, where
a source serves their domain or a stable identifier already stored.  Where they are absent,
resolution proceeds on provisional validity.  The order is then the database,
the batch cache, and datasources. A guess is produced only where what the source stated
leaves the instrument or its listing open, and only after both lookups have missed.
Corporate events are fetched for the batch's resolved instruments once resolution
completes.

A candidate that agrees with a guess ranks below one that confirms stated data or
corroborates a higher precedence answer.  It ranks above the datasource's own order.

## Undecided

- Whether a datasource whose answer overlapped an existing instrument only on a
  MIC-derived identifier is barred from contributing to that instrument in later runs.
