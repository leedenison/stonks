---
title: Massive identifier events
type: task
---

## Scope

Massive answers for the entity a symbol names. Its answer covers each ticker in the
entity's chain over the interval the ticker named the entity. The provider facts are in
[001](001-open-m05.md).

In:

- The client for the ticker events endpoint. One fetch per stable identifier.
- Conversion of each reply to identifier facts: events, assertions and value coverage, for
  every operating MIC that Massive lists. See
  [011](../adr/011-massives-identifier-facts-are-written-for-every-us-venue-over-the-history-window.md).
  The oldest event of a chain is an assignment when it falls inside the request period.
  An oldest event on 2003-09-10 or at the start of the window marks where Massive's data
  begins.
- The point in time lookup, which asserts a value on the later of the key's earliest
  transaction and the start of the history window. See
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md).
- `delisted_utc` from the ticker listing with `active=false`, as a witnessed retirement.
- `Classify` tells an entitlement 403 apart from a 403 for dates outside the history
  window. Only the first blocks the datasource. See
  [010](../adr/010-a-refusal-of-the-requested-dates-fails-only-the-request.md).
- The rate of the reference bucket. Massive does not state its limit, so each plan's
  rate stands in for it.
- Tests against recorded traffic redacted as it is saved, including a key whose ticker
  changed and a retired ticker.
