---
title: Massive identifier events
type: task
status: unreviewed
dependencies: [002]
---

## Scope

Massive answers for the entity a symbol names. Its answer covers each ticker in the
entity's chain over the interval the ticker named the entity.

In:

- The client for Massive's identifier event endpoint, tested against recorded traffic
  redacted as it is saved.
- Conversion of each answer to events, assertions and value coverage.
- One fetch per stable identifier, covering the earliest transaction date to the present.
- End to end coverage through vcrproxy of a key whose ticker changed.
