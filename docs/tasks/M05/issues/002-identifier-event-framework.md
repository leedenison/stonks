---
title: Identifier events, assertions and coverage
type: task
status: unreviewed
---

## Scope

The records that validity is derived from, and the fetch kind that writes them.

In:

- An identifier event fetch kind beside identity.
- Events keyed on the natural key of a MIC_TICKER. An event is witnessed or inferred, and
  references the run that gave rise to it.
- Assertions, as rows of the fetch key that made them. An identity fetch asserts each
  identifier its answer returns.
- Coverage of a value and of a domain. Only a fetch key with a served outcome writes it.
  The period of a domain fetch follows
  [001](../adr/001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md).
- Validity of a value to one instrument. The framework derives it from the three and
  caches it. A new row that affects a cached validity invalidates it.
- An inferred event. The framework writes one when two assertions of one value name
  different instruments.
- Normalisation of a provider's spelling of a symbol before an event or an assertion is
  recorded.
