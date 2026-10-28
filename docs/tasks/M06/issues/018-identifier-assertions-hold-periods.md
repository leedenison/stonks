---
title: Identifier assertions hold periods
type: task
---

An identifier assertion claims what a ticker names over a period of days. Each bound of
the period is an edge or open. Of two answers from one source, the later wins. See
[014](../adr/014-an-identifier-assertion-holds-a-period.md).

## Scope

In:

- The schema of `identifier_assertions` and `identifier_assertion_coverage`.
- Derivation of ticker validity, and the findings it raises.
- The writers: the Massive ticker history and lookup, resolution, reassertion and
  identifier attestation.
- The admin API, the admin Tickers tab and run page, and the e2e specs.
- The deferred text that describes edges.

Out:

- Edge-only assertions are deferred with D-IDENT.
- `AttestIdentifierRequest` has an inclusive `to`, because the admin form asks for a
  last day.
