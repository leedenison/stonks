---
title: Coverage and attestation in the admin area
type: task
dependencies: [002, 007]
---

## Scope

An administrator sees where coverage is missing and attests it. See
[006](../adr/006-an-administrator-attests-what-no-datasource-covers.md).

In:

- A coverage page in the admin area, by domain. A timeline shows datasource coverage,
  attestations and gaps. Gaps are ordered by the held keys and transactions they affect.
- From a gap, one action attests the domain or a value over the gap's period. The action
  is prefilled, and the administrator adds a note.
- An action to attest an assertion over an interval, and one to record an event.
- The same attest action beside a provisional key on the statement details page.
- Withdrawing an attestation, which recomputes validity and replays the keys it affects.
