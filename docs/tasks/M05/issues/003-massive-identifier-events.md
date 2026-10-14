---
title: Massive identifier events
type: task
status: unreviewed
dependencies: [002]
---

## Scope

Massive answers for the entity a symbol names. Its answer covers each ticker in the
entity's chain over the interval the ticker named the entity. The provider facts are in
[001](001-open-m05.md).

In:

- The client for the ticker events endpoint. One fetch per stable identifier.
- Conversion of each answer to events, assertions and value coverage. The oldest event
  of a chain is an assignment.
- The point in time lookup, which asserts a value on the later of the key's earliest
  transaction and the start of the history window. See
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md).
- `delisted_utc` from the ticker listing with `active=false`, as a witnessed retirement.
- A 404 from the ticker overview, for a value Massive asserted before, as a negative
  assertion. See [004](../adr/004-a-not-found-answer-is-evidence-of-retirement.md).
- `Classify` tells an entitlement 403 apart from a 403 for dates outside the history
  window. Only the first blocks the datasource.
- The rate limit of the reference bucket, in place of the stocks rate.
- Tests against recorded traffic redacted as it is saved, including a key whose ticker
  changed and a retired ticker.
- End to end coverage through vcrproxy of a key whose ticker changed.
