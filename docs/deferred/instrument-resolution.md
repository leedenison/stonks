---
title: Instrument resolution
recorded: 2026-09-20
---

# Instrument resolution

Answers what a source states about an instrument from the configured datasources, so that
the names different sources use collapse onto one instrument.

## Why

Brokers name instruments in their own terms.  A key states what one broker called a
line, so one security held at two brokers is two keys, and holdings, prices and events
cannot be aggregated across them until a datasource answers for the identifiers those
keys state.

## Model

### Instruments and Listings

An instrument has one listing per currency family, so a source quoting a listing in GBp/GBX
and one quoting it in GBP name one listing.  Stored prices, identifiers and transactions
each keep the precise currency code they were stated in.

Venues are treated as display only metadata when they are available.  Listings across
venues are considered fungible.  It will often be the case that a given datasource for
price information will quote prices on a different venue from the one that is actually
traded anyway.

A listing carries the interval it was tradeable in. A delisting closes one. A
redenomination closes one and merges what it holds into the listing taking over.

A currency instrument's listing in another currency carries the rate between the two, and
is made when a rate is first fetched.  Whether GBX is a listing of the GBP instrument or
an instrument of its own is the currency family question below.

An option or a future references the listing of its underlying, a strike being quoted
in the listing's currency.

### Identifiers

A venue is named by its ISO 10383 MIC, normalised to the operating MIC through a
reference MIC table seeded from the published list by a checked in generator.  The domain
of a MIC_TICKER is the operating MIC, and that domain is the only way a source states a
venue.

Resolution may query a datasource with a symbol a source stated without its venue, but
never associates on one.  The weakest link rule then leaves a key stating nothing else
unresolved, and nothing replays it, since no coverage can arrive for a key without a
domain.

### Validity and the Weakest Link

A MIC-derived identifier is trusted inside its validity interval, which is confirmed
inside identifier event coverage and provisional outside it.  See
[identifier-events.md](identifier-events.md).  It associates a key with an instrument
when the key's transactions are dated inside its validity, but never decides between two
instruments and never merges them.

The weakest link governs.  A key stating only a ticker is associated through that ticker
however many stable identifiers a datasource answers with, so the association is
provisional until the ticker is covered.

An identifier row exists only from a datasource assertion.  A stated identifier no
datasource answered for stays in the key, and a key nothing answered for stays
unresolved: its holdings aggregate with the user's other unresolved keys on the
identifiers they share, and nothing about it is stored against an instrument.

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

### Resolution Is Incremental

The ability of the system to resolve instrument data will vary depending on what
datasources are configured and enabled as well as what the user provides with their
statements.  Some instruments might be available and complete, others might only be
partially available and others still might be completely unavailable.

A user's holdings are therefore some part keys resolved to instruments and some part
keys nothing answered, and a key moves between the two as answers arrive.

### Merger of Instruments

Two datasource answers, or an answer and the database, may describe one instrument.  They
merge only when they share a stable identifier.  Answers that overlap only on a
MIC-derived identifier are not merged: precedence picks the instrument kept, the
identifiers of the other are dropped, and the outcome is recorded as a finding of the
run.

### Contradictions are Resolved Automatically and Recorded

Two datasources may contradict each other's instrument data.  Contradictions are resolved
automatically: datasources carry an explicit precedence and are consulted in precedence
order.  Non-overlapping data from two datasources is merged provided no datum is
contradictory and at least one stable identifier links the two.

Despite the automatic resolution each contradiction is recorded as a finding of the
resolution that met it.  See [runs.md](runs.md).

### Re-Resolution

Datasources gain coverage, integrations are enabled and quota tiers change, so an
unresolved instrument is re-attempted by a scheduled replay and on administrator demand.

A key is re-resolved from what it states, so a later answer moves its association to the
instrument the answer names.  An identifier event that leaves the key's transactions
dated outside the validity it was associated under replays it.  Holdings, event grouping
and cached adjusted values derived from the key's transactions are recomputed.

### Datasources

The datasource framework constraints apply, keyed on the stated key.  An absence of
instrument data is tolerated by distinguishing an instrument nothing recognised from one
whose identification was unavailable, and attempting re-resolution later.

## Invariants

### An Identifier Names One Instrument At A Time

No two instruments hold one identifier triple over overlapping validity intervals.

### No Merge Through a MIC-derived Identifier

Instruments merge only through a stable identifier, so no identifier event unwinds a
merge.

## Sketch

Identifier events for the MIC-derived identifiers a batch states are fetched first, where
a source serves their domain or a stable identifier already held.  Their absence blocks
nothing; resolution proceeds on provisional validity.  The order is then the database,
the batch cache, and datasources. A guess is produced only where what the source stated
leaves the instrument or its listing open, and only after both lookups have missed.
Corporate events are fetched for the instruments the batch resolved to once resolution
completes.

Datasource answers are held apart rather than flattened into one set, since what makes an
association a claim is that one source stated both halves of it. One answer supplies the
instrument's metadata, and the others contribute what they are admitted to contribute.

### Choosing Among Datasource Answers

Every enabled datasource is asked concurrently, each as a fetch with the resolution as
parent.  Once all have returned:

1. Each datasource returns its candidates.  A datasource asked about one identifier may
   find several listings, since a bare ticker names a listing at every venue that quotes
   the symbol.  The integration converts each to the canonical shape, since only it knows
   which provider field carries the asset class or that a market-wide listing names a
   market rather than a venue, and declares what the call strictly filtered on.  It does
   not rank them.
2. Candidates are dropped that do not name the queried identifier, that contradict
   stated data, or that are inconsistent with the answer chosen for a higher precedence
   datasource.
3. Each datasource's remaining candidates are ranked: confirms stated data, then
   corroborates a higher precedence answer, then agrees with a guess, then the
   datasource's own order with a market-wide listing preferred over an arbitrary venue.
4. The winner is the top candidate of the highest precedence datasource with any
   candidate left.  It supplies the instrument's metadata.
5. Each losing datasource contributes its top candidate where it corroborates the
   winner: its identifiers, and the fields the winner left blank.

Datasources are taken in precedence order through steps 2 and 3, so each is ranked
against answers already chosen above it.

The winner is decided by precedence alone.  A lower precedence datasource that confirmed
more of the stated data does not displace it, since its answer enriches the winner's
where it corroborates.

A guess ranks and never filters.

The resolution records how many candidates each datasource offered, which step dropped
each one and on what grounds, and which tier chose the survivor.  A candidate dropped as
inconsistent and one dropped as uncorroborated are different findings.

### Agreement

Naming: a candidate names an identifier by returning it or by strictly filtering on it.
Answering a strictly filtered call at all asserts the value names the instrument
described, whether or not the datasource echoes it back.  A fuzzy search that answers a
query for one symbol with another instrument's listing has neither returned nor
filtered on the symbol.

Confirming stated data: contradicting no stated field and confirming at least one, being
a currency, an asset class the candidate corroborates by the stated class lying strictly
under the candidate's in the class tree, or an identifier of the same type and domain.
A candidate naming a venue no stated identifier names has answered about a different
listing and confirms nothing.  A candidate too sparse to be checked against
anything neither contradicts nor confirms.

Consistency: two answers describe one listing and do not contradict each other.  The
currency decides whether they describe one listing, and the venue decides only where a
currency is absent.  Identifiers contradict when both name one subject, the same type
and domain, with different values.  An identifier the other answer also named is
agreement, and agreement anywhere in that answer settles it.

Corroboration: at least one stable identifier is named by both.  Agreeing on a
MIC-derived identifier is the query restated rather than evidence about the instrument,
because the identity a resolution starts from is routinely reassigned and each datasource
may have found several instruments the query admits.

Filling: a value the winner holds is never replaced.  The asset class is never filled
from another answer, since it decides which invariants the instrument must satisfy.

## Undecided

- Whether a datasource whose answer overlapped an existing instrument only on a
  MIC-derived identifier is barred from contributing to that instrument in later runs.

- Which currencies form one family, and whether a family is anything more than a unit
  prefix.

- Whether a candidate ranked up because it corroborates a higher precedence answer is
  independent corroboration of that answer.  A broad search may contain a candidate
  matching almost anything, and the naming and contradiction checks a candidate must
  pass on its own are what limit that.  Whether they limit it enough is open.

- What happens when every candidate from every datasource contradicts the stated data.
  Dropping them leaves the instrument unresolved, where a broker that mis-states a
  currency would otherwise resolve with the contradiction recorded.  Whether a wrong
  statement should block resolution or only be recorded is open.
