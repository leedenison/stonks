---
title: Holdings read a transaction's instrument
type: task
dependencies: [005]
---

## Scope

Holdings give each transaction of a dated key the instrument its value names on its
date, derived at the read. See
[014](../adr/014-a-transactions-instrument-is-derived-when-read.md) and
[016](../adr/016-a-transaction-is-unresolved-where-its-value-names-nothing-on-its-date.md).

In:

- Fixed keys are summed by instrument in SQL. The transactions of dated keys
  are listed with their value, order date, quantity and currency, and summed in Go after
  one derivation per distinct value.
- A transaction that is unresolved on its date is summed into its key's group. A dated key
  joins a group on its stated identifiers.
- The listing of a dated transaction is the one that carries the ticker on its date, or
  the instrument's listing in the stated family where the families differ.
- A holding's name is the current ticker, whatever the dates beneath it.
- Tests cover a key whose transactions straddle a rename, a retired value, and a dated
  key beside a fixed key of the same instrument.
