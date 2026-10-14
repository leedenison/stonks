---
title: Coverage and attestation in the admin area
type: task
dependencies: [007]
---

## Scope

An administrator sees where coverage is missing and attests it. See
[006](../adr/006-an-administrator-attests-what-no-datasource-covers.md).

In:

- A coverage page in the admin area, by domain. A timeline shows datasource coverage,
  attested assertions and gaps. Gaps are ordered by the held keys and transactions they
  affect.
- From a gap of a value, one action attests the value on the gap's first day. The action
  is prefilled.
- An action to attest that a value names an instrument, or names nothing, on a day. It
  may take a range and write an assertion on each end.
- The same attest action beside a provisional key on the statement details page.
- Withdrawing an attestation, which recomputes validity and replays the keys it affects.
