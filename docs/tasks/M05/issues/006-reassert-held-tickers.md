---
title: An administrator extends the validity of held tickers
type: task
---

## Scope

An administrator starts an extend validity run over every held ticker from the admin area,
beside the runs it creates. The run extends each bracket to the present. See
[005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md).

In:

- Each enabled datasource that serves a held ticker receives one call for it, as
  [005](../adr/005-validity-is-bootstrapped-from-the-present-backwards.md) states. Each
  answer writes its assertion on the day of the answer.
- Extending validity bypasses the fetch cache, since a cached answer holds on the day of the
  original call.
- The fetch runs as a system run. The run shows the assertions it wrote. Replays and
  findings follow as the `replay` and `resolve` package comments state.
- An extend validity run is a system run. It asks again about an instrument that
  `identity_coverage` records, and writes an identity answer's assertion outside
  `attach`, as [019](../adr/019-extending-validity-writes-only-the-held-tickers-assertions.md)
  states. The `resolve` package comment and the `identity_coverage` comment record the
  same behaviour.
- Tests:
  - A dbtest in `resolve` over scripted datasources. One declines a ticker on a non-US
    venue, and the other answers it. The assertion falls on the pinned day. Confirmed
    validity moves from an older assertion to the present.
  - A unit test in `market`. A warm cache entry does not stop a call that extends validity.
  - End to end coverage through vcrproxy. A fixture seeds a past-dated assertion of a
    held ticker, and an administrator extends validity. The system run lists a
    present assertion of the ticker, on the UTC day the run started.
