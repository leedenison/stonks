---
title: Instrument resolution
recorded: 2026-09-20
---

# Instrument resolution

How resolution handles the life of a listing, options and the validity of a MIC-derived
identifier, and the replays that keep an association current.

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

A broker states a contract as an OPTION identifier: a symbol in the OCC format, built
from the root, expiry, right and strike the export states, whatever venue lists the
contract.  Only a datasource that lists the contract adds an OCC symbol to the
instrument.  OpenFIGI takes the stated symbol as an OCC_SYMBOL.  Massive takes it under
its O: prefix.  OpenFIGI, Massive and EODHD's options API cover US contracts only, so a
contract listed elsewhere keeps its stated symbol until a datasource covers it.

A contract's terms are stored on the instrument apart from its identifiers: underlying,
expiry, right, strike and shares per contract, in current terms.  A re-resolution
rebuilds the symbol to send from them.

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

Massive's options contracts endpoint states a contract's underlying ticker, expiry, right,
strike and shares per contract, and is the source of the canonical contract.

### Authority

Instruments, listings and identifiers are system owned and written only from a source
with system authority.  A source with user authority, a statement or an annotation,
writes keys and what the user records against them, and never instrument data.  A
source with candidate authority, any guess, does not write instrument data.  Its rank may
decide an association, which then records the guess for the user to confirm or revise.
See [guesses.md](guesses.md).

## Constraints

### Merger of Instruments

Two datasource answers, or an answer and the database, may describe one instrument.  They
merge only when they share a stable identifier.  Where answers overlap only on a
MIC-derived identifier, they are not merged: precedence picks the instrument kept, the
identifiers of the other are dropped, and the outcome is recorded as a finding of the
run.

When a datasource's answer overlaps an instrument only on a MIC-derived identifier,
resolution asks that datasource again in later runs. Another route may add to the
instrument a stable identifier that the datasource's answer carries, and the answer then
merges through it.

### Corroboration

An answer corroborates only the claims it states itself. Where the ranking raised a
candidate for agreeing with a higher precedence answer, the candidate corroborates the
identifiers, class and listings it restates.

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
the batch cache, and datasources.  A guess is produced where they leave the instrument
or its listing open.  See [guesses.md](guesses.md).  Corporate events are fetched for the
batch's resolved instruments once resolution completes.
