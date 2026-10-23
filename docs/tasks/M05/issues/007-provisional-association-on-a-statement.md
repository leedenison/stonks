---
title: A statement shows a provisional association
type: task
---

## Scope

The statement details page marks each transaction of a dated key as confirmed,
provisional or unresolved, and each transaction of a provisional fixed association as
provisional. See
[003](../adr/003-a-provisional-association-shows-on-the-statement-not-the-holding.md)
and
[016](../adr/016-a-transaction-is-unresolved-where-its-ticker-names-nothing-on-its-date.md).

In:

- End to end coverage through vcrproxy. A statement states META in 2024, and a second
  states META on 2021-06-01. The statement details page marks the 2021 transaction
  unresolved, since Massive's chain shows that META began naming Meta on 2022-06-09 and
  a lookup on 2021-06-01 finds nothing.
