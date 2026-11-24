---
title: Confirm the as-at basis of Schwab quantities
type: task
---

## Scope

Confirm before the first release whether a Schwab transaction history restates the
quantity of a line by a later split. The marshaller assumes that Schwab states each
quantity as traded, so it states each row as at its own date. See the file comment in
[schwab.ts](../../../../client/lib/marshal/schwab.ts). If Schwab restates each quantity
to the export date, the system restates a trade before a split a second time, and its
holding is wrong.

The Schwab exports on hand cannot settle the question. Each export holds a single split,
and every line of the split's symbol comes after that split.

The evidence needs a line of one symbol on each side of a split, in both the JSON and
the CSV export. Schwab's own documentation of the history would also serve.

If Schwab restates quantities, each row is as at the date of the export, and the
marshaller and its tests change to match.

## Design

Nothing settled.
