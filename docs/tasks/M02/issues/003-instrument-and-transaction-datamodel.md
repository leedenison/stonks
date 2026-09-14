---
title: Instrument and transaction datamodel
type: task
---

## Scope

The migrations, queries and shared proto types for instruments, listings, identifiers,
transactions and stated keys.

In:

- Instrument, with an asset class and an owner.
- Listing, one per currency family of an instrument, with an owner.
- Identifier, at instrument or listing grain, with a type, an optional domain, a value
  and an owner.
- The cash instrument, seeded with a listing per supported currency and a currency
  identifier on each; see
  [../adr/006-cash-is-one-instrument-with-a-listing-per-currency.md](../adr/006-cash-is-one-instrument-with-a-listing-per-currency.md).
- Transaction, with order date, settlement date, "as at" date, quantity, currency, the
  upload that wrote it and the stated key it was resolved from.
- Stated key, one row per distinct key per upload.
- The asset class tree and the full identifier type vocabulary, in SQL and in
  `stonks.type.v1`, though only the broker description and currency types are
  exercised. Broker description is listing grain; see
  [../adr/005-a-broker-description-is-a-listing-grain-identifier.md](../adr/005-a-broker-description-is-a-listing-grain-identifier.md).

Out:

- Validity intervals on identifiers. Validity is derived and cached, never stored as a
  fact.
- Coverage, provenance and any reference to a fetch.
- Prices, corporate events, identifier events, correlations and events grouping
  transactions.

## Design

Surrogate keys are version 7 UUIDs minted by the server.

An instrument has zero or more listings.

No two instruments hold one identifier triple for one owner. A user owns an instrument,
listing or identifier that only their own uploads support. The seeded cash rows are
system owned and nothing else is. A user owned parent has children owned by the same
user only.

A transaction attaches to an instrument, and to a listing when its currency is known.
Quantity and money are exact decimals.

A stated key holds what the source stated about the instrument: identifiers, asset class,
currency, venue and description. It is stored with the upload so a later resolution can
be replayed from it.
