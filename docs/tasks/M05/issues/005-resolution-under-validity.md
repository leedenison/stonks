---
title: Resolution under identifier validity
type: task
status: unreviewed
dependencies: [002]
---

## Scope

In:

- Before a batch is resolved, identifier events are fetched where a source serves the
  domain or a stable identifier concerned. Resolution proceeds when they are absent.
- A MIC-derived identifier associates a key only when the key's transactions are dated
  inside its validity. An association is provisional when it rests on provisional
  validity.
- When a run associates a key or makes a fetch on the assumption that a value has not
  moved, it records the assumption against itself.
- An event finds the keys whose transactions now fall outside the validity of their
  association, and those keys are replayed from their stated keys.
- Where two providers assert different instruments for one value at one moment,
  precedence decides. See [002](../adr/002-precedence-decides-between-providers-that-disagree-and-a-finding-records-it.md).
- A witnessed event inside a bracket of same-instrument assertions is recorded as a
  finding of the run that met it.
