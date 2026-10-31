---
title: Confirm the as-at basis of Schwab quantities
type: task
---

## Scope

Confirm before the first release whether a Schwab transaction history restates the
quantity of a line by a later split. The marshaller states every row as at the export
date. See the file comment in
[schwab.ts](../../../../client/lib/marshal/schwab.ts). If Schwab states each quantity
as traded, a trade before a split is never restated, and its holding is wrong.

The Schwab exports on hand cannot settle the question. Each export holds a single split,
and every line of the split's symbol comes after that split.

The evidence needs a line of one symbol on each side of a split, in both the JSON and
the CSV export. Schwab's own documentation of the history would also serve.

If Schwab states quantities as traded, each row is as at its own date, and the
marshaller and its tests change to match.

## Design

Nothing settled.
