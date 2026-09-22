---
title: Datasource framework
type: task
---

## Scope

The framework every fetch from an external provider goes through: what an integration
declares, what a fetch records, and what stops a call being made.

In:

- The integration interface: the kinds of data and the keys it serves, answered locally
  without spending quota; the identifier it sends for a key; the classification of a
  failure as temporary or permanent; and its map between the provider's venue codes and
  operating MICs.
- The registry of integrations, each with an enabled state and a precedence, read from
  configuration so any instance may run a different combination.
- The fetch kind of run: one datasource, one kind of data, a set of keys and a period.
  Per key it records the outcome, served, not served or failed temporarily or
  permanently, the identifier sent, the instrument the answer was attached to and the
  identifiers that answer named. Those identifiers are the fetch's assertions: at the
  moment of the fetch, the datasource said each names the instrument it described. A
  candidate the resolution dropped was attached to nothing, asserts nothing and is
  recorded as a finding, not as identifiers.
- Coverage per key, per datasource and per period, written only from a served key. A
  range answer is recorded against the domain rather than against each value in it.
- Blocks: a permanent failure for a key the integration declared it serves, one row per
  key, datasource and kind, suppressing further calls until an administrator clears it.
- A rate limiter per datasource.

Out:

- Any integration, and the finding row and admin surface; issues
  [005](005-findings-and-the-run-admin-surface.md) and
  [007](007-the-first-identity-integration.md).
- The schedule trigger.

## Design

Rows are the record. Assertions are rows of the fetch key that made them, never fields
on the identifier: the identifiers table holds the conclusion, one triple naming one
subject, while assertions hold the conflicting claims an inferred identifier event is
found from. A stored row references the fetch key that produced it, the fetch
key carries the identifier sent and the fetch the moment it was sent, so every row
resting on an identifier that later proves to have moved is found from the event without
searching the data. A run is kept for as long as anything references it. A truncated
fetch covers nothing.

```sql
fetch(run_id, datasource, kind, period, fetched_at)
fetch_key(id, run_id, key, outcome, instrument_id, sent_type, sent_domain, sent_value)
fetch_identifier(fetch_key_id, type, domain, value)
```
