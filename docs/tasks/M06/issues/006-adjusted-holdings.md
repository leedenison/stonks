---
title: Adjusted holdings
type: task
dependencies: [003, 004, 008, 012, 018, 019]
---

## Scope

Holdings show the quantity of an instrument restated to today.

In:

- Restating each transaction on read from one datasource and market. See
  [001](../adr/001-a-corporate-action-is-stored-against-its-ticker-and-attributed-through-ticker-validity.md)
  and [002](../adr/002-a-transaction-reflects-corporate-actions-up-to-its-as-at-date.md).
- Read each transaction from `txn_instrs` as one row with its as-at date.
- The coverage of each transaction, and the adjusted or unadjusted state of each
  holding. See [003](../adr/003-a-holding-is-adjusted-only-when-covered-to-today.md).
- The trait in `internal/types` that marks a class that corporate actions restate.
- The stated split check in `corpact` skips a split whose instrument's class lacks the
  trait.
- The restated quantity, the state and the uncovered period in the holdings API.
  Rewrite the comments in `holding.proto` and the `holding` package that state
  quantities are raw.
- The holdings page and the holding detail.
- An e2e spec that restates a holding across a split, with Massive replayed through the
  recording proxy.

## Decisions

- The zero rule drops a holding when the quantity it shows is zero. That is the restated
  sum of an adjusted holding and the raw sum of any other.
- A group holding is not tied to an instrument, so it shows its raw quantity without the
  mark.
- `HoldingKey.quantity` is in the holding's units. It is restated when the holding is
  adjusted and raw otherwise. Each key is rounded on its own.
- The restated sum is rounded half away from zero to the finest scale that any of the
  holding's transaction quantities states. Trailing zeros are then trimmed.
