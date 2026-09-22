---
title: Keys resolve to instruments
type: task
---

## Scope

Instruments, listings and identifiers become system owned only, and a stated key carries
its own resolution, so a transaction reaches its instrument through its key and a key no
datasource answered is a holding in its own right.

In:

- Removing owner_id from instruments, listings and identifiers, with the owner chain and
  immutability triggers and the per owner uniqueness of identifiers. An identifier
  triple names one subject.
- Removing the broker description identifier type. The description is a field of the
  key, and a stated identifier becomes an identifier row only from a datasource answer.
- The association on the stated key: the instrument and listing resolution named, the
  identifier it associated through, and whether that validity is confirmed or
  provisional. Transactions lose their instrument and listing columns and reach both
  through the key.
- The group of a stated key: the user's unresolved keys that share an identifier, or a
  description within one broker and channel, are one group, transitively. It is
  recomputed whenever a user's keys change and stored on the key.
- The holdings query and RPC answering two kinds of holding: of an instrument, summed
  over every key resolved to it, and of a group, summed over its keys and carrying the
  identifiers and descriptions they state. Cash is a holding of the currency instrument.
- The resolution outcomes matched, rejected and unresolved. Every key that states no
  currency is unresolved until issue
  [008](008-resolution-against-datasources.md) gives resolution a datasource to ask.
- The migration and package comments rewritten for the model.
- An e2e spec uploading two brokers' exports that state one identifier and asserting one
  holding.

Out:

- Datasources and any resolution beyond the database; issue
  [008](008-resolution-against-datasources.md).
- Anything a user records against a key: a pin to an instrument, a grouping, a price, or
  a key made by hand.

## Design

A user never states anything about an instrument. Instruments exist only from a
datasource assertion, and the seeded currencies. A user states keys, which are scoped to
the user, and resolution is the mapping from a key to an instrument, absent where nothing
answered. Holdings take the user at their word for every key nothing answered, and the
datasource's word for every key one did: an unresolved key never groups with a resolved
one, since the identifier they share is one the datasource did not admit.

Moving an association is one update on a key and never rewrites the transaction log.
