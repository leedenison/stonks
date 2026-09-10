---
title: Instrument resolution
recorded: 2026-09-04
---

# Instrument resolution

Takes the identifiers and metadata a source states about an instrument and answers with
the instrument the system already holds, or with what the system can determine from its
configured datasources.

## Why

Anything a user can hold in their portfolio is modelled as an instrument.  Brokers name
instruments in their own terms, and holdings, prices and events can't be aggregated until
those names collapse onto one instrument.

## Model

### Instruments and Listings

An instrument has one listing per currency family, so a source quoting a listing in GBp/GBX
and one quoting it in GBP name one listing.  Stored prices, identifiers and transactions
each keep the precise currency code they were stated in.  Instruments in reality must have
at least one listing, but may have no listings associated with them in the datamodel
indicating a lack of knowledge about that instrument's listings.

Venues are treated as display only metadata when they are available.  Listings across
venues are considered fungible.  It will often be the case that a given datasource for
price information will quote prices on a different venue from the one that is actually
traded anyway.

Transactions, prices, corporate events, etc attach to both an instrument and a listing
when fully qualified.  This allows them to represent either precise knowledge when a
currency is known, or the imprecise knowledge when a currency is not known for the
attached data.

A listing carries the interval it was tradeable in. A delisting closes one. A
redenomination closes one and merges what it holds into the listing taking over.

Cash is modelled as an instrument where the balance is held in the currency listing of
the instrument.  An FX pair is the instrument a rate is a price of. Each has one
degenerate listing.

An option or a future references the listing of its underlying, a strike being quoted
in the listing's currency.

### Identifiers

A broker's own description of an instrument is an identifier type, and the marshaller
constructs a domain unique to the broker and the channel.  Broker description identifiers
ensure uploads of the same broker description are matched in the database without expensive
calls to external services.  Within one domain and for one owner a description names one
instrument at a time.

### Asset Class

Every source states an asset class at whatever specificity it can defend, so a datasource
that cannot distinguish a stock from an ETF states the class both fall under rather than
choosing one.

Classes are not compared for equality. Generally we want to determine if classes
contradict; the sets implied by two values are disjoint.  Or we want to determine if
classes corroborate; the stated class is strictly a subset of that found in
authoritative datasources.

### Identifier Type

Scope says whether a value can be recognised outside the channel that supplied it, and
whether a domain is needed to qualify it.

Reassignment decides whether an identifier can be trusted without identifier event
coverage for the interval it is used in.

- **Rare**: retired on use and never reassigned, or reassigned only by documented
  exception.  Trusted without coverage.
- **Routine**: reassigned in the ordinary course.  Trusted only inside a validity
  interval, which identifier event coverage bounds.  A routine identifier associates a
  transaction with an instrument only when the transaction date lies inside its validity,
  and links two instruments for a merge only when the moments both claims were asserted
  do.  Inside its validity it is as trustworthy as a rare identifier.
- **Unverifiable**: no datasource can witness a reassignment.  Assumed never reassigned
  within its domain.  Associates a transaction with an instrument, and absorbs an
  instrument that holds no other identifier, but never decides between two instruments
  that verifiable identifiers can decide between.

The weakest link governs.  A transaction stating only a ticker is associated via that
ticker however many rare identifiers a datasource answers with, so the ticker needs
coverage.

An identifier row exists only when the identifier is usable.  A routine identifier stated
without coverage is held only in the stated key stored with the transaction, from which
resolution is replayed when coverage arrives.

### Ownership

A user owns instruments, listings and identifiers where they have uploaded data which could
not be confirmed or contradicted by a more authoritative source.  Everything else is system
owned.

If instruments are considered the parent node with instrument identifier and listing children,
with listings in turn having listing identifier children then we can say:

- A system owned parent may have system owned children.
- A system owned parent may have user owned children.
- A user owned parent may have user owned children only when the users are equal.
- A user owned parent may **NOT** have system owned children.

### Authority

System owned data is updated directly from a source with system authority.

A source with user authority may freely update user owned data belonging to the uploading
user.  Updates to system owned data must first be corroborated by a system authority
source.  Any channel which is user authenticated to a non-admin user is limited to user
authority.

A source with candidate authority may update user owned data belonging to the user
authenticated in the current request where that user implicitly or explicitly agrees to
give authority to the guess, upgrading it, but may not override or contradict the values
the user supplied.  Updates to system owned data must first be corroborated by a system
authority source.  Any LLM driven source of data is limited to candidate authority.

## Constraints

### Resolution Is Incremental

The ability of the system to resolve instrument data will vary depending on what
datasources are configured and enabled as well as what the user provides with their
uploads.  Some instruments might be available and complete, others might only be
partially available and others still might be completely unavailable.

The system must therefore accommodate instrument data which is some part system owned,
some part user owned and some part missing entirely.  This is why user owned instrument
data exists (See the `Ownership` section).  It is also why the system accommodates
a variable number of identifiers associated with each instrument or listing, and why it
accommodates instruments that have no listings associated with them.

### Attachment or Merger of Instruments

When we have two sets of instrument data which claim to be the same instrument, for
example a set of data in the database and a set of data supplied by a user which share
one or more identifiers, we must mediate when the data can be merged.

Data with candidate authority is not stored.  Data with user authority is stored as user
owned data.  Data with system authority is stored as system owned data.  The shape of the
resulting data must conform with the rules laid out in the `Ownership` section.

### Unresolved is a First Class State

An instrument which was not identified by any source beyond a broker description is still
created and stored.  Holding quantities may be modified for the instrument via
transactions. It is otherwise treated as a valid instrument like any other.

### Contradictions are Resolved Automatically and Recorded

Two sources may contradict each other's instrument data.  In general the contradictions
are resolved automatically according to the following rules:

- System authoritative sources take precedence over user authoritative sources, and both
  over candidate authoritative sources.
- Sources with the same level of authority must have an explicit precedence and are
  consulted in precedence order.

Non-overlapping data from sources with the same level of authority can be merged
provided:

- No datum in the sets provided by the sources is contradictory.
- At least one valid identifier authoritatively links the two instruments.

Despite the automatic resolution the contradictions themselves are recorded as rows of the
resolution run that met them, and reported to admin users from those rows.

### Re-Resolution

Datasources gain coverage, integrations are enabled and quota tiers change, so an
unresolved instrument is re-attempted periodically and on administrator demand.

A transaction is re-resolved from the stated key stored with it, so a later answer moves
the transaction to the instrument the answer names.

### Datasources

The datasource framework constraints apply, keyed on the stated key.  An absence of
instrument data is tolerated by:

- Storing user owned instruments under what the source did state.
- Distinguishing an instrument nothing recognised from one whose identification was
  unavailable and attempting re-resolution later.

## Invariants

### User or Candidate Authority Data is Upgraded When Possible

Instrument data from a source with candidate authority is never stored directly unless it
is confirmed by a higher authority.

Instrument data from a source with user authority is stored as user owned data unless it
is confirmed by a higher authority.

Instrument data from a source with system authority is stored as system owned data.

The system always attempts to upgrade the authority of sourced data before deciding
whether and how to store it.

Note: Determining the authority to upgrade can be subtle.  If a candidate authority
source guesses an identifier (eg. a CUSIP) associated with a broker description and a
listing currency, a system authority source can upgrade the association between the
CUSIP and the guessed listing currency.  But a separate, probably user authority source,
is needed to upgrade the association between the CUSIP and the broker description.

### An Identifier Names One Instrument At A Time

No two instruments hold one identifier triple over overlapping validity intervals for one
owner.

## Sketch

Resolution is keyed on what the source stated rather than on the transaction, so one key
is resolved once per batch and every transaction carrying it receives the same answer.

Identifier events for the routine identifiers the batch states are fetched first, so
each identifier's validity is known before any lookup.  The order is then the database,
the batch cache, and datasources. A guess is produced only where what the source stated
leaves the instrument or its listing open, and only after both lookups have missed.
Corporate events are fetched for the instruments the batch resolved to once resolution
completes.

Datasource answers are held apart rather than flattened into one set, since what makes an
association a claim is that one source stated both halves of it. One answer supplies the
instrument's metadata, and the others contribute what they are admitted to contribute.

### Choosing Among Datasource Answers

Every enabled datasource is asked concurrently.  Once all have returned:

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

The run records how many candidates each datasource offered, which step dropped each
one and on what grounds, and which tier chose the survivor.  A candidate dropped as
inconsistent and one dropped as uncorroborated are different findings.

### Agreement

Naming: a candidate names an identifier by returning it or by strictly filtering on it.
Answering a strictly filtered call at all asserts the value names the instrument
described, whether or not the datasource echoes it back.  A fuzzy search that answers a
query for one symbol with another instrument's listing has neither returned nor
filtered on the symbol.

Confirming stated data: contradicting no stated field and confirming at least one, being
a currency, an asset class the candidate corroborates, or an identifier of the same type
and domain.  A candidate naming a venue no stated identifier names has answered about a
different listing and confirms nothing.  A candidate too sparse to be checked against
anything neither contradicts nor confirms.

Consistency: two answers describe one listing and do not contradict each other.  The
currency decides whether they describe one listing, and the venue decides only where a
currency is absent.  Identifiers contradict when both name one subject, the same type
and domain, with different values.  An identifier the other answer also named is
agreement, and agreement anywhere in that answer settles it.

Corroboration: at least one instrument-grain identifier is named by both.  Agreeing on
the listing is the query restated rather than evidence about the instrument, because
the identity a resolution starts from is routinely ambiguous and each datasource may
have found several instruments the query admits.

Filling: a value the winner holds is never replaced.  The asset class is never filled
from another answer, since it decides which invariants the instrument must satisfy.

A resolution run records the outcome of each key it resolved as its own rows. The mix of
outcomes for one run is then a query, which is what makes two runs over the same input
comparable and lets a test assert that a change has not disturbed the flow. Scoping a count
to one run means an unbounded metric attribute, and a test asserts on rows it can read
rather than on telemetry that is absent whenever no collector is configured.

### Proposed Asset Classes

```
UNKNOWN
|-- CASH
`-- SECURITY
    |-- EQUITY
    |   |-- STOCK
    |   |-- ETF
    |   `-- MUTUAL_FUND
    |-- FIXED_INCOME
    |-- DERIVATIVE
    |   |-- OPTION
    |   `-- FUTURE
    `-- FX
```

### Proposed Identifier Types

| Type                 | Scope      | Domain               | Grain      | Reassignment |
| -------------------- | ---------- | -------------------- | ---------- | ------------ |
| ISIN                 | registry   | none                 | instrument | rare         |
| CUSIP                | registry   | none                 | instrument | rare         |
| CINS                 | registry   | none                 | instrument | rare         |
| WERTPAPIER           | registry   | none                 | instrument | rare         |
| OPENFIGI_SHARE_CLASS | registry   | none                 | instrument | rare         |
| SEDOL                | registry   | none                 | listing    | rare         |
| OPENFIGI_COMPOSITE   | registry   | none                 | listing    | rare         |
| MIC_TICKER           | registry   | venue                | listing    | routine      |
| OPENFIGI_TICKER      | registry   | venue                | listing    | routine      |
| OCC                  | registry   | none                 | instrument | routine      |
| OPRA                 | registry   | none                 | instrument | routine      |
| FUT_OPT              | registry   | none                 | instrument | routine      |
| CURRENCY             | registry   | none                 | instrument | rare         |
| FX_PAIR              | registry   | none                 | instrument | rare         |
| DATASOURCE_TICKER    | datasource | datasource           | listing    | routine      |
| BROKER_ID            | broker     | broker               | instrument | rare         |
| BROKER_DESCRIPTION   | source     | broker + upload type | instrument | unverifiable |

## Undecided

- What happens to a user owned instrument left holding no transactions when
  re-resolution moves them to another instrument.

- Whether resolution answers "which instrument holds this identifier now" or "which
  instrument held it on the transaction's date". The second is what the validity interval
  exists for, and the first is what a lookup by value naturally does.  We need to
  determine whether there are use cases for the second.

- Which currencies form one family, and whether a family is anything more than a unit
  prefix.

- What a user does when they believe the system has identified an instrument wrongly, and
  what an administrator can correct that a user cannot.

- Whether a candidate ranked up because it corroborates a higher precedence answer is
  independent corroboration of that answer.  A broad search may contain a candidate
  matching almost anything, and the naming and contradiction checks a candidate must
  pass on its own are what limit that.  Whether they limit it enough is open.

- What happens when every candidate from every datasource contradicts the stated data.
  Dropping them leaves the instrument unresolved, where a broker that mis-states a
  currency would otherwise resolve with the contradiction recorded.  Whether a wrong
  statement should block resolution or only be recorded is open.
