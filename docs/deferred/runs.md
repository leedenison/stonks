---
title: Runs
recorded: 2026-09-20
---

# Runs

The replay kind of work, the schedule trigger, and starting a run from the admin
surface.

## Why

A replay takes longer than the request starting it can wait, and answers per key rather
than as a whole.  Some work is started by no user: a replay after an event, or one an
administrator asks for.

## Model

### Kinds

- A replay re-resolves the transactions an event or new coverage has affected.

### Triggers

A run is started by a schedule as well as by a user, an administrator or another run.
Provenance says what produced a row.  Lineage says why the work happened: a resolution
contains the fetches it sent, and a replay caused by an event names the fetch that
recorded the event.

### Findings

A stated split the calendar lacks and an event the system cannot handle are findings of
the runs that meet them.  An unhandled corporate event is a row of its own, reported by a
finding that references it.

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

The admin surface starts a run.

### Resolution Against Datasources

A key resolved against a datasource takes seconds, and the system owned instruments it
creates are shared across users.  Read-only resolution therefore proceeds in parallel.
Creation is serialised per stated key, with an advisory lock keyed on it, so two runs
stating one key produce one instrument.  The write of transactions stays ordered.

With more than one process, pending runs are claimed from the database with `SKIP LOCKED`
where no earlier non-terminal run shares its user and lane.

## Undecided

- Whether clearing the finding on an unhandled event is what clears the event.

- Whether a run can be cancelled, and whether an interrupted run is resumed or restarted
  once its payload is persisted.

- How overlapping runs that touch one instrument are ordered, such as a scheduled replay
  starting during a user's upload.
