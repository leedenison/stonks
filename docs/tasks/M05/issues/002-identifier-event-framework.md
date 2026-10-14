---
title: Identifier events, assertions and coverage
type: task
---

## Scope

The records that validity is derived from, and the fetch kind that writes them.

In:

- An identifier event fetch kind beside identity.
- Events keyed on the natural key of a MIC_TICKER. An event assigns, renames or retires a
  value. It is witnessed or inferred, and references the run that gave rise to it.
- Assertions, as rows of the fetch key that made them. An assertion holds at a moment or
  over an interval. An identity fetch asserts each identifier its answer returns.
- Negative assertions, and the inferred retirement that one bounds. See
  [004](../adr/004-a-not-found-answer-is-evidence-of-retirement.md).
- Coverage of a value and of a domain. Only a fetch key with a served outcome writes it.
  The period of a domain fetch follows
  [001](../adr/001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md).
  The lag is a setting of each datasource.
- Validity of a value to one instrument. The framework derives it from the three and
  caches it. A new row that affects a cached validity invalidates it.
- An inferred event. The framework writes one when two assertions of one value name
  different instruments.
- Coverage gaps, as
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md) states.
- An administrator as the source of an assertion, coverage or an event. See
  [006](../adr/006-an-administrator-attests-what-no-datasource-covers.md).
- System runs, which act for the system rather than for a user. See
  [008](../adr/008-an-event-replays-its-keys-beneath-a-system-run.md).
- Normalisation of a provider's spelling of a symbol before an event or an assertion is
  recorded.
