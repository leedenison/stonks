---
title: Resolution under identifier validity
type: task
dependencies: [003]
---

## Scope

In:

- Before a batch is resolved, identifier events are fetched where a source serves the
  domain or a stable identifier concerned. Resolution proceeds when they are absent.
- A MIC-derived identifier associates a key only when the key's transactions are dated
  inside its validity. An association is provisional when it rests on provisional
  validity.
- `stated_keys.validity` is a cache of the validity of the key's transactions. New
  coverage, an event or an assertion recomputes it. See
  [007](../adr/007-an-event-finds-what-it-affects-from-the-association.md).
- When a run writes an assertion, coverage or an event, it finds the keys whose
  transactions now fall outside the validity of their association. It derives validity
  before and after the write to find them, and replays them at once. See
  [008](../adr/008-an-event-replays-its-keys-beneath-a-system-run.md).
- Where two providers assert different instruments for one value on one day, the run
  that met the disagreement records a finding. See
  [002](../adr/002-precedence-decides-between-providers-that-disagree-and-a-finding-records-it.md).
- A witnessed event inside a bracket of same-instrument assertions is recorded as a
  finding of the run that met it.
- End to end coverage through vcrproxy of a key whose ticker changed.
