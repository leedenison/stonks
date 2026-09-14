---
title: Runs
recorded: 2026-09-14
---

# Runs

The unit of work through which uploads are ingested, instruments are resolved, data is
fetched and earlier answers are replayed, and the findings each piece of work records.

## Why

An upload, a resolution and a fetch each take longer than the request starting them can
wait, each answers per item rather than as a whole, and each meets contradictions it must
record.  The container is the same for every kind of work, so it is recorded once here and
each kind records only what fills it.

## Model

### Runs

A run is a row created before its work starts.  It is the identity a caller holds while
the work proceeds and reads progress and the outcome back against.  It carries its kind,
its trigger, the run that started it, its state, and when it started and finished.

- An upload ingests one batch of transactions.
- A resolution answers a set of stated keys.
- A fetch asks one datasource for one kind of data over one period.
- A replay re-resolves the transactions an event or new coverage has affected.

### Triggers

A run is started by a user, by an administrator, by a schedule or by another run.  A run
started by another run names it as parent: an upload contains the resolution it started,
the resolution contains the fetches it sent, and a replay caused by an event names the
fetch that recorded the event.  Provenance says what produced a row.  Lineage says why the
work happened.

### Items

Each kind owns the shape of its per-item row: a transaction accepted or rejected for an
upload, a stated key and the answer chosen for a resolution, a key and whether the
datasource served it for a fetch.  The mix of outcomes for one run is a query, which makes
two runs over the same input comparable and lets a test assert that a change has not
disturbed the flow.

The items of an upload are addressed to the user who made it.  Findings are addressed to
the administrator.

### Findings

A finding is a row recording something a run met that an administrator may need to see: a
contradiction resolved by precedence, a candidate dropped, a stated split the calendar
lacks, an event the system cannot handle.  It references the run that met it and the rows
it is about, and carries its kind and whether an administrator has cleared it.

A finding is informational when the run decided the matter and stored data, and blocking
when something is withheld until an administrator acts.  A finding never changes
behaviour on its own.  A block on a key and an unhandled corporate event are rows of
their own, each reported by a finding that references it.

## Constraints

### Work is Background

The call starting a run answers with the run rather than with its result.  Progress, the
outcome, the items and the findings are read against the run.

### Rows are the Record

Item rows and findings are the record a run leaves, and the admin surface and tests read
them.  Telemetry carries a bounded mirror of counts per kind, trigger and outcome.
Scoping a count to one run would be an unbounded metric attribute, and telemetry is
batched, expired and absent whenever no collector is configured, so nothing is driven
from it.

### Interruption

A run whose process dies is left in its last state with the items it had written.  Each
kind states what a partial run means: a truncated fetch covers nothing.  See
[datasources.md](datasources.md).

## Invariants

### A Run Outlives its References

Provenance and coverage reference fetch keys, findings reference runs, and a fetch key
reaches its run through the fetch.  A run row is kept for as long as anything references
it.

## Sketch

```sql
run(id, kind, trigger, parent_id, state, started_at, finished_at)
fetch(run_id, datasource, kind, period, fetched_at)
fetch_key(id, run_id, key, outcome, instrument_id, sent_type, sent_domain, sent_value)
fetch_identifier(fetch_key_id, type, domain, value)
resolution_key(run_id, stated_key, outcome, ...)
finding(id, run_id, kind, consequence, subject, cleared_at)
```

The framework owns the run row, the finding row, the admin surface and the telemetry
mirror.  Each kind owns its item rows and the rows it stores.

The admin surface lists runs by kind, trigger and state, starts a run, and lists and
clears findings.

## Undecided

- Whether clearing the finding on a block or an unhandled event is what clears the block,
  or the two are cleared separately.

- Whether a finding on one subject met by two runs is one finding or two.

- Whether a run can be cancelled, and whether an interrupted run is resumed, restarted or
  left for an administrator.

- How overlapping runs that touch one instrument are ordered, such as a scheduled replay
  starting during a user's upload.

- Which findings are shown to the user whose upload met them, such as a broker statement
  contradicted by a datasource.
