---
title: Coverage and attestation in the admin area
type: task
---

## Scope

An administrator sees where coverage is missing and attests it. See
[006](../adr/006-an-administrator-attests-what-no-datasource-covers.md).

In:

- A coverage page in the admin area, by domain. A timeline shows datasource coverage,
  attested assertions and gaps. Gaps are ordered by the held keys and transactions they
  affect.
- From a gap of a ticker, one action attests the ticker on the gap's first day. The action
  is prefilled.
- An action to attest that a ticker names an instrument, or names nothing, on a day. It
  may take a range and write an assertion on each end.
- The same attest action beside a provisional key on the statement details page.
- Withdrawing an attestation. Its assertion is deleted. The unresolved keys that state the
  ticker are replayed beneath the attestation run. A dated key's transactions read the
  change at the next read.
