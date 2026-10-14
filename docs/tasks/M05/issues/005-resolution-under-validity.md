---
title: Resolution under identifier validity
type: task
dependencies: [002]
---

## Scope

In:

- Before a batch is resolved, identifier events are fetched where a source serves the
  domain or a stable identifier concerned. Resolution proceeds when they are absent.
- A MIC-derived identifier associates a key only when the key's transactions are dated
  inside its validity. An association is provisional when it rests on provisional
  validity.
- `stated_keys.validity` is a cache of the validity of the key's transactions. New
  coverage, an assertion or an attestation recomputes it. See
  [007](../adr/007-an-event-finds-what-it-affects-from-the-association.md).
- An event finds the keys whose transactions now fall outside the validity of their
  association. It replays them at once, beneath the run that recorded it. See
  [008](../adr/008-an-event-replays-its-keys-beneath-a-system-run.md).
- Where two providers assert different instruments for one value at one moment,
  precedence decides. See [002](../adr/002-precedence-decides-between-providers-that-disagree-and-a-finding-records-it.md).
- A witnessed event inside a bracket of same-instrument assertions is recorded as a
  finding of the run that met it.
