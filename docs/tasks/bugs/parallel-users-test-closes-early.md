---
title: TestParallelUsers closes the runner before its runs complete
type: bug
---

`TestParallelUsers` in `server/internal/run` fails intermittently with gomock reporting
a missing call to `CompleteRun`.

## Cause

The test waits until both runs have begun, then calls `Close()`. Each run's work waits on
`both` or on cancellation. When `Close()` cancels the runner first, `select` may pick the
cancellation. The runner does not record a run whose work stops after cancellation, so
`CompleteRun` is never called and the mock's expectation fails.

## Fix

Wait for both `CompleteRun` calls before closing, for example by having the mock signal
on each call.
