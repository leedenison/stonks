---
title: Runs
recorded: 2026-09-20
---

# Runs

The fetch and replay kinds of work, and the findings every run records for an
administrator.

## Why

A fetch and a replay each take longer than the request starting them can wait, and each
answers per key rather than as a whole.  Each also meets contradictions it must record: a
contradiction resolved by precedence, a candidate dropped, a stated split the corporate
event calendar lacks and an event the system cannot handle all pass unseen without a
record addressed to an administrator.

## Model

### Kinds

- A replay re-resolves the transactions an event or new coverage has affected.

### Triggers

A run is started by an administrator or by a schedule as well as by a user or another run.
Provenance says what produced a row.  Lineage says why the work happened: a resolution
contains the fetches it sent, and a replay caused by an event names the fetch that
recorded the event.

### Findings

A finding is a row recording something a run met that an administrator may need to see: a
contradiction resolved by precedence, a candidate dropped, a stated split the calendar
lacks, an event the system cannot handle.  It references the run that met it and the rows
it is about, and carries its kind and whether an administrator has cleared it.  The items
a run writes are addressed to the user whose work made them; findings are addressed to the
administrator.

A finding is informational when the run decided the matter and stored data, and blocking
when something is withheld until an administrator acts.  A finding never changes
behaviour on its own.  A block on a key and an unhandled corporate event are rows of
their own, each reported by a finding that references it.

## Constraints

### Rows are the Record

Findings are the record a run leaves for an administrator, and the admin surface and tests
read them.  Telemetry carries a bounded mirror of counts per kind, trigger and outcome.
Scoping a count to one run would be an unbounded metric attribute, and telemetry is
batched, expired and absent whenever no collector is configured, so nothing is driven
from it.

### Interruption

Each kind states what a partial run means.  A truncated fetch covers nothing.  See
[datasources.md](datasources.md).

## Invariants

### A Run Outlives its References

Provenance and coverage reference fetch keys, findings reference runs, and a fetch key
reaches its run through the fetch.  A run row is kept for as long as anything references
it.

## Sketch

```sql
finding(id, run_id, kind, consequence, subject, cleared_at)
```

The framework owns the finding row, the admin surface and the telemetry mirror.  Each kind
owns its item rows and the rows it stores.

The admin surface lists runs by kind, trigger and state, starts a run, and lists and
clears findings.

### Resolution Against Datasources

A key resolved against a datasource takes seconds, and the system owned instruments it
creates are shared across users.  Read-only resolution therefore proceeds in parallel.
Creation is serialised per stated key, with an advisory lock keyed on it, so two runs
stating one key produce one instrument.  The write of transactions stays ordered.

With more than one process, pending runs are claimed from the database with `SKIP LOCKED`
where no earlier non-terminal run shares its user and lane.

## Undecided

- Whether clearing the finding on a block or an unhandled event is what clears the block,
  or the two are cleared separately.

- Whether a finding on one subject met by two runs is one finding or two.

- Whether a run can be cancelled, and whether an interrupted run is resumed or restarted
  once its payload is persisted.

- How overlapping runs that touch one instrument are ordered, such as a scheduled replay
  starting during a user's upload.

- Which findings are shown to the user whose statement met them, such as one contradicted
  by a datasource.
