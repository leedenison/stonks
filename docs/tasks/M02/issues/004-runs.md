---
title: Runs
type: task
---

## Scope

The run framework: the row a piece of background work is read against, how the work is
executed, and the API that reads it back.

In:

- The run row: kind, trigger, parent, state, error, and created, started and finished
  times. The states are pending, running, completed, failed and interrupted; see
  [../adr/007-a-run-is-pending-running-completed-failed-or-interrupted.md](../adr/007-a-run-is-pending-running-completed-failed-or-interrupted.md).
- The kinds upload and resolution, and the triggers user and run. A run started by
  another run names it as parent.
- In-process background execution of a run after the call starting it has answered. A
  run of one user and broker waits for every earlier run of the same user and broker to
  finish; see
  [../adr/008-runs-of-one-user-and-broker-proceed-in-creation-order.md](../adr/008-runs-of-one-user-and-broker-proceed-in-creation-order.md).
- A sweep at boot that moves every pending and running run to interrupted.
- An RPC reading a run and its items, scoped to the user who started it.

Out:

- Findings and the admin surface.
- The schedule trigger, fetches and replays.
- The telemetry mirror of run counts.

## Design

The call starting a run answers with the run, and progress, the outcome and the items are
read against it.

Each kind owns the shape of its per-item row. The mix of outcomes for one run is a query.

A parent's state is its own. An upload whose resolution failed fails itself.

The payload is held in memory, so an interrupted run is neither resumed nor restarted.
