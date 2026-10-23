---
title: Resolution under identifier validity
type: task
---

## Scope

A key the resolver associates through a MIC-derived identifier takes a dated
association, and its transactions read the instrument the value names on their dates.
See [013](../adr/013-an-association-is-fixed-or-dated.md) and
[015](../adr/015-a-ticker-row-names-the-present-holder-and-resolution-reads-validity.md).

In:

- A dated association names a `MIC_TICKER` value instead of an instrument. A CHECK
  allows exactly one of the two.
- Resolution reads a venue ticker's validity before it asks a datasource. When the facts
  already confirm a key's dates, the resolver associates the key without a fetch. When a
  key's value has no facts, the resolver sends it through the identity fetch.
- Identifier events are fetched over the history window for each instrument that the
  identity fetch answered for a key, where a source serves one of its stable
  identifiers. Resolution proceeds when they are absent.
- A statement that states a key with a dated association asserts the value at the
  earliest of the key's dates that lacks confirmed validity. A past-dated answer that
  names an instrument the system lacks creates it with its stable identifiers and
  without a ticker row. A present-dated answer that names a holder other than the
  row's moves the ticker row.
- `validity.Record` maps an assertion to an instrument through stable identifiers only.
- A run that writes a fact replays the unresolved keys that state the value, beneath the
  run that wrote it. See
  [008](../adr/008-a-new-fact-replays-the-unresolved-keys-that-state-its-value.md).
- Where two providers assert different instruments for one value on one day, the run
  whose write completes the pair records a finding. See
  [002](../adr/002-precedence-decides-between-providers-that-disagree-and-a-finding-records-it.md).
- A witnessed event inside a bracket of same-instrument assertions is recorded as a
  finding of the run that wrote the event.
- A dated key refuses a user's choice, as a fixed key does. The refusal reads the
  `via_*` columns as well as `instrument_id`. See
  [017](../adr/017-a-users-choice-pins-an-unassociated-key.md).
- End to end coverage through vcrproxy of a key whose ticker changed, asserted through
  the API. A statement states META in 2024 and the key takes a dated association. A
  second statement states META on 2021-06-01. The first statement's resolution fetches
  Meta's chain and a lookup, and the second's fetches one lookup and skips the identity fetch.

Out:

- Reading a transaction's instrument on the holdings page, which is issue
  [013](013-holdings-read-a-transactions-instrument.md).
- The per-transaction mark on the statement page, which is issue
  [007](007-provisional-association-on-a-statement.md).
