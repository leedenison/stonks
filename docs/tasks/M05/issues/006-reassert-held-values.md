---
title: An administrator re-asserts held values
type: task
dependencies: [005]
---

## Scope

An administrator starts a re-assertion of every held value from the admin area, beside
the runs it creates. The re-assertion extends each bracket to the present. See
[005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md).

In:

- Each enabled datasource that serves a held value receives one call for it, as
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md) states. Each
  answer writes its assertion on the day of the answer.
- A re-assertion bypasses the fetch cache, since a cached answer holds on the day of the
  original call.
- The fetch runs as a system run. The run shows the assertions it wrote. Replays and
  findings follow as [005](005-resolution-under-validity.md) states.
- A re-assertion runs as a system run. It asks again about an instrument that
  `identity_coverage` records, and writes an identity answer's assertion outside
  `attach`. The `resolve` package comment and the `identity_coverage` comment state
  this.
- Tests:
  - A dbtest in `resolve` over scripted datasources. One declines a value on a non-US
    venue, and the other answers it. The assertion falls on the pinned day. Confirmed
    validity moves from an older assertion to the present.
  - A unit test in `market`. A warm cache entry does not stop a re-assertion call.
  - End to end coverage through vcrproxy. A fixture seeds a past-dated assertion of a
    held value, and an administrator starts the re-assertion. The system run lists a
    present assertion of the value, on the UTC day the run started.

Open:

- Whether an identity answer to a re-assertion writes anything beyond its assertions,
  such as the listings and identifiers that `attach` writes.
