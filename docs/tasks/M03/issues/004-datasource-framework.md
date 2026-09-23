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
  failure as temporary or permanent and as being about the identifier or the datasource;
  and its map between the provider's venue codes and operating MICs. It is called with
  a batch of identifiers and answers per identifier, since a provider charges and rate
  limits per request, and chunks the batch to whatever the provider accepts.
- The registry of integrations, a table each instance seeds with the datasources it
  holds credentials for, carrying an enabled state, a precedence, the credential and the
  endpoint, so any instance may run a different combination.
- The fetch kind of run: one datasource, one kind of data and a set of keys. Per key it
  records the outcome, served, not served, blocked or failed temporarily or permanently,
  the identifier sent, the calls made, the instrument the answer was attached to and the
  identifiers the answer returned. Those identifiers are the fetch's assertions: at the
  moment of the fetch, the datasource said each names the instrument it described. A
  candidate the resolution dropped was attached to nothing, asserts nothing and is
  recorded as a finding, not as identifiers.
- Blocks: one row per datasource and kind, scoped either to the identifier the call was
  sent under or to the whole datasource, suppressing further calls until an
  administrator clears it.
- A rate limiter per datasource, and the backoff a temporary failure is retried under.

Out:

- Any integration, and the finding row and admin surface; issues
  [005](005-findings-and-the-run-admin-surface.md) and
  [007](007-the-first-identity-integration.md).
- Coverage. It is owned by the consumer of each kind of data, so identity coverage lands
  in issue [008](008-resolution-against-datasources.md).
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

A fetch records no period. A period qualifies one key rather than the call, and the
coverage a fetch earns is the consumer's to record.

```sql
datasources(name, enabled, precedence, credential, endpoint)
fetches(id, user_id, datasource, kind)
fetch_keys(id, fetch_id, user_id, stated_key_id, outcome, attempts, instrument_id,
    sent_type, sent_domain, sent_value, reason)
fetch_identifiers(fetch_key_id, type, domain, value)
datasource_blocks(id, datasource, kind, scope, sent_type, sent_domain, sent_value,
    reason, fetch_key_id, cleared_at)
```
