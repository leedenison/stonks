---
title: Runs
type: task
---

## Scope

The run framework: the row a piece of background work is read against, how the work is
executed, and the API that reads it back.

In:

- The run row: kind, trigger, parent, state, started and finished times.
- The kinds upload and resolution, and the triggers user and run. A run started by
  another run names it as parent.
- In-process background execution of a run after the call starting it has answered.
- An RPC reading a run and its items, scoped to the user who started it.

Out:

- Findings and the admin surface.
- The schedule trigger, fetches and replays.
- The telemetry mirror of run counts.

## Design

The call starting a run answers with the run, and progress, the outcome and the items are
read against it.

Each kind owns the shape of its per-item row. The mix of outcomes for one run is a query.

A run whose process dies is left in its last state with the items it had written.
