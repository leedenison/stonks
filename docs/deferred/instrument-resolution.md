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
instrument.

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

Reassignment decides whether an identifier can be trusted without corporate event coverage
for the period it is used in.  Some types are retired on use and never reassigned; some are
reassigned only by documented exception.

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

## Shared Datasource Constraints

### On-Demand Fetch

We generally have no datasource that offers bulk fetching of instrument data.  We
therefore need to fetch instruments held by users on demand covering the period the
instrument was held.  The datamodel must be able to express which instruments we have
successfully fetched, what time periods they cover and whether retrieval of a
particular instrument from a particular datasource failed temporarily or permanently.

### Fetching from Multiple Sources

We generally have no datasource that offers instrument data for the complete range
of instruments we want to accommodate (due to coverage limits on geography, asset class,
historic period, etc per datasource).  The system must fetch instruments from an
ensemble of datasources.

### Minimizing Fetch Cost

Fetching data from a datasource is generally expensive due to API quota limits, rate
limits, etc.  The system should avoid fetching duplicate data when more than one
source covers the same instrument.  The system should also avoid making API calls which
are guaranteed to fail because the datasource is known not to provide the instruments
requested.

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
system must tolerate a temporary or permanent absence of instrument data by:

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

The order is the database first, then the batch cache, then datasources. A guess is
produced only where what the source stated leaves the instrument or its listing open, and
only after both lookups have missed.

Datasource answers are held apart rather than flattened into one set, since what makes an
association a claim is that one source stated both halves of it. One answer supplies the
instrument's metadata, and the others contribute what they are admitted to contribute.

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
| BROKER_DESCRIPTION   | source     | broker + upload type | instrument | routine      |

## Undecided

- Instrument resolution and corporate event fetching each need the other. Resolution has
  to know a reassignable identifier's corporate events to tell which instrument holds it
  over which interval; corporate event fetching has to name a resolved instrument to
  record events against. Which side gives, and what the first pass over a new instrument
  is allowed to assume, is open.

- Whether resolution answers "which instrument holds this identifier now" or "which
  instrument held it on the transaction's date". The second is what the validity interval
  exists for, and the first is what a lookup by value naturally does.  We need to
  determine whether there are use cases for the second.

- Which currencies form one family, and whether a family is anything more than a unit
  prefix.

- What a user does when they believe the system has identified an instrument wrongly, and
  what an administrator can correct that a user cannot.
