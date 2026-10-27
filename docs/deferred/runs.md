---
title: Runs
recorded: 2026-09-20
---

# Runs

Runs that an identifier event, a corporate action, new coverage or a schedule starts, and
runs spread over several processes.

## Why

An identifier event, a corporate action or new coverage can leave an association stale
while no user is present. The association stays stale until an administrator starts a
replay.

## Model

### Kinds

- A replay re-resolves the transactions that an identifier event, a corporate action or
  new coverage has affected.

### Triggers

A schedule starts a run.  When an identifier event or a corporate action causes a replay,
the replay names the fetch that recorded it.

### Findings

A stated split the calendar lacks and a corporate action the system cannot handle are
findings of the runs that meet them.

## Constraints

### Interruption

Each kind states what a partial run means.  A truncated fetch covers nothing.  See
[datasources.md](datasources.md).

## Invariants

### A Run Outlives its References

Provenance and coverage reference fetch keys, findings reference runs, and a fetch key
reaches its run through the fetch.  A run row is kept for as long as anything references
it.

## Sketch

With more than one process, pending runs are claimed from the database with `SKIP LOCKED`
where no earlier non-terminal run shares its user and lane.

## Undecided

- Whether clearing the finding on an unhandled corporate action is what clears the action.

- Whether a run can be cancelled.
