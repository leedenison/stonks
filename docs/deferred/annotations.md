---
title: Annotations
recorded: 2026-10-23
---

# Annotations

What a user records against their own stated keys: a choice of instrument, a grouping, a
price, and a key made by hand for an asset no source states.

## Why

An unresolved key is still a holding, and the user knows things about it that no source
can state: which instrument it is, that two keys are one holding, what it is worth. Some
assets, a house or a mortgage, are stated by no source at all. Where no datasource covers
a holding, the system works on data the user enters. In a multi-user system that knowledge
applies to the user's own holdings. It is recorded against keys, which are the user's.
Instruments are shared, so an annotation stays off them.

## Model

Every annotation is keyed on the user and an identifier triple or a key, so it survives
the replacement of keys a re-upload performs. An annotation is user data. A choice writes
neither an identifier row nor an assertion.

### Choices

A choice names the instrument, and where the key leaves it open the listing, that the
transactions of one key belong to, for that user alone. It fills in what the facts leave
open. A user who thinks a datasource is wrong is correcting it, and correction is an
administrator's work: an identifier attestation, a mapping of a broker description, or an
identifier added to an instrument. A choice is offered only where the facts leave a
transaction's instrument open.

The shape of a choice follows the strongest identifier the key states, as resolution
does: a stable identifier fixes a key, and a venue ticker alone dates it.

- A stable identifier that no datasource answered. The choice is fixed: one instrument
  for every transaction of the key. The user picks an instrument the system has, or
  searches OpenFIGI and confirms a hit. The hit is a datasource answer, so it creates the
  instrument with system authority and the stable identifiers it carries, and a later
  answer for the same identifier merges through them. A datasource that serves the
  identifier's type and answered not found has not answered.
- A stable instrument-grained identifier also leaves the listing open. The user settles
  the instrument first. Where a datasource covers the instrument, every listing it serves
  is fetched and offered, which is the listing review of [guesses.md](guesses.md). Where
  none does, the key's stated currency is the holding's currency and the user's price
  series values it. The instrument stays without a listing.
- A venue ticker. Inside confirmed ticker validity the facts decide. Outside it, the user
  chooses an instrument for a run of the key's transactions. The choice is a row on the
  key. Deriving a transaction's instrument reads it before ticker validity, so the system
  facts stay the only source of ticker validity. The user selects transactions, and the
  range follows from their dates: every transaction for all time, or the provisional ones
  for the gap since the last assertion.
- A bare ticker. The user supplies the venue, as an identifier added to the key, and the
  key becomes a venue ticker.
- A key that states only a description or an option symbol. The choice is fixed.

A choice that settles an ambiguity is offered only for a key that states a broker
description. The ambiguities are a listing, a run of transactions and a venue. A broker
names a line once, so a key with a description names one thing, and the choice says what
that thing is. Without a description, two transactions of one key may name different
listings at one moment, and a choice cannot tell them apart. Such a key is rare, and the
user cannot choose for it.

A fixed choice is untouched by an event, as every fixed association is. A choice stands
when coverage grows or a datasource later answers the key's identifier.

### Other annotations

- A user joins two holdings no source stated in common by adding an identifier to a
  key. Grouping gathers the keys sharing an identifier, so this needs no grouping of its
  own and is honoured every time groups are recomputed. The user cannot exclude a key
  from a group. A key that wrongly joins two holdings is a defect in the export, and the
  user corrects the export.
- A price is a value on a date for one identifier triple, in the currency the key states.
  A holding's user price series is the union over its identifiers' series, the latest
  entry winning on a date.
- A manual key is created by hand through a channel of its own, with transactions
  entered against it, for assets and liabilities no source states. Their asset classes
  sit at the root of the tree. Every integration declines them, so they stay local.

## Sketch

Valuation splits by class. Cash and liability value at quantity, so a mortgage is a
liability holding in a currency valued at its balance, with interest entered as
transactions. Everything else values at quantity times price, from the instrument's
prices where the key resolved and from the user's series otherwise.

The choice for a venue ticker is made on the statement details page, which marks each
transaction confirmed, provisional or unresolved. The user selects the marked
transactions and names the instrument.

Working on manual data alone, with no datasource covering the user's holdings, is large
enough to be a milestone of its own.

## Undecided

- Whether a user may make a negative choice: that a venue ticker named nothing over a run
  of their transactions. The case for it is a retired ticker reused for a line the user
  never bought. Provisional extension merges the later transactions into the old holding,
  and a negative choice splits them into the unresolved group.
- Whether a not found from a mapping API on a stable identifier is an authoritative
  negative that refuses the choice, or a miss. A provider's universe is not the world, so
  the miss reading is the working one.
- Whether a replay that finds a datasource answer for a key with a fixed choice records a
  finding for the user. The user's holding and every other user's then show different
  instruments, and the two stay apart.
- Whether an administrator may add an identifier to an instrument. It is the counterpart
  of the fixed choice, and it would serve every user.
- Whether system prices take precedence over a user's series for a resolved key, or fill
  only the dates the user left blank.
- The currency of a group whose keys state codes of one family, GBP and GBX.
- Whether a choice informs resolution's ranking of candidates for other users' keys, or
  stays entirely private.
- Whether interest on a liability is generated from a rule or always entered.
