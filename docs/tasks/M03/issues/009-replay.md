---
title: Replay
type: task
dependencies: [008]
---

## Scope

Re-resolving stated keys after the answer available for them may have changed.

In:

- The replay kind of run, started by an administrator over the keys whose outcome was
  unavailable, or over every key a newly enabled datasource serves.
- Re-resolution of each key, so a later answer moves the key's association, its group is
  recomputed, and the holdings derived from its transactions follow.
- The admin action starting a replay, and the replay read on the runs page.

Out:

- The schedule trigger. Every replay in this milestone is started by an administrator.
- Replay caused by an identifier event or by corporate event coverage.
