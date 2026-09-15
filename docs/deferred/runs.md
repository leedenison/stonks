---
title: Runs
recorded: 2026-09-14
---

# Runs

The unit of work through which statements are ingested, instruments are resolved, data is
fetched and earlier answers are replayed, and the findings each piece of work records.

## Why

A statement, a resolution and a fetch each take longer than the request starting them can
wait, each answers per item rather than as a whole, and each meets contradictions it must
record.  The container is the same for every kind of work, so it is recorded once here and
each kind records only what fills it.

## Model

### Runs

A run is a row created before its work starts.  It is the identity a caller holds while
the work proceeds and reads progress and the outcome back against.  It carries its kind,
its trigger, the run that started it, its state, and when it started and finished.

- A statement ingests one batch of transactions.
- A resolution answers a set of stated keys.
- A fetch asks one datasource for one kind of data over one period.
- A replay re-resolves the transactions an event or new coverage has affected.

### Triggers

A run is started by a user, by an administrator, by a schedule or by another run.  A run
started by another run names it as parent: a statement contains the resolution it started,
the resolution contains the fetches it sent, and a replay caused by an event names the
fetch that recorded the event.  Provenance says what produced a row.  Lineage says why the
work happened.

### Items

Each kind owns the shape of its per-item row: a transaction accepted or rejected for a
statement, a stated key and the answer chosen for a resolution, a key and whether the
datasource served it for a fetch.  The mix of outcomes for one run is a query, which makes
two runs over the same input comparable and lets a test assert that a change has not
disturbed the flow.

The items of a statement are addressed to the user who made it.  Findings are addressed to
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

A run whose process dies is marked interrupted with the items it had written.  Each kind
states what a partial run means: a statement writes its transactions and items in one
database transaction, so an interrupted statement has written none; a truncated fetch covers
nothing.  See [datasources.md](datasources.md).

### Ordering

Runs of one user and broker proceed in creation order, since a statement replaces a period
and the later statement is the one to keep.  Runs of different users or brokers proceed in
parallel.  Receipt is the RPC and never waits.

## Invariants

### A Run Outlives its References

Provenance and coverage reference fetch keys, findings reference runs, and a fetch key
reaches its run through the fetch.  A run row is kept for as long as anything references
it.

## Sketch

```sql
run(id, kind, trigger, parent_id, state, error, created_at, started_at, finished_at)
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

### Resolution Against Datasources

A key resolved against a datasource takes seconds, and the system owned instruments it
creates are shared across users, so ordering per user and broker no longer covers it.
Read-only resolution proceeds in parallel; creation is serialised per stated key, with an
advisory lock keyed on it, so two runs stating one key produce one instrument.  The write
of transactions stays ordered per user and broker.

With more than one process, pending runs are claimed from the database with
`SKIP LOCKED` where no earlier non-terminal run shares the user and broker.

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
