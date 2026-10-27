---
title: Adjusted holdings
type: task
dependencies: [003, 004, 008, 012]
---

## Scope

Holdings show the quantity of an instrument restated to today.

In:

- Restating each transaction on read from one datasource and market. See
  [001](../adr/001-a-corporate-event-is-stored-against-its-ticker-and-attributed-through-validity.md)
  and [002](../adr/002-a-transaction-reflects-corporate-events-up-to-its-as-at-date.md).
- Read the transactions of a fixed key one row at a time, with their as-at dates,
  instead of summing them in `ListHoldingKeys`.
- Read the held instruments, the held ticker dates and the attribution of each
  transaction through one set of queries, shared with `refreshcorporateevents`.
- The coverage of each transaction, and the adjusted or unadjusted state of each
  holding. See [003](../adr/003-a-holding-is-adjusted-only-when-covered-to-today.md).
- The trait in `internal/types` that marks a class that corporate events restate.
- The stated split check in `corporate` skips a split whose instrument's class lacks the
  trait.
- The restated quantity, the state and the uncovered period in the holdings API.
  Rewrite the comments in `holding.proto` and the `holding` package that state
  quantities are raw.
- The holdings page and the holding detail.
- An e2e spec that restates a holding across a split, with Massive replayed through the
  recording proxy.

## Open questions

- Whether the zero rule drops a holding by its raw sum or by its restated sum. A buy of
  100, a two for one split and a sale of 200 sum to -100 raw and to zero restated.
- Whether a group holding, which has no instrument, carries the unadjusted mark.
- Whether `HoldingKey.quantity` is raw or restated.
- The scale the restated sum is rounded to. The API states a quantity as an exact
  decimal, and a restated sum may not terminate.
