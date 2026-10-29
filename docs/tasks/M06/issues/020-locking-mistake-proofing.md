---
title: Locking mistake-proofing
type: task
---

## Scope

This issue makes the locking scheme harder to misuse. The isolation level it depends on is pinned. Every transaction retries after a
deadlock or a concurrent write. A rebuild of ticker validity cannot run unless the
transaction holds the ticker rows. One query takes every advisory lock, under names that
one file builds.

Out of scope: the corporate action answer lock as an upsert, and any change to the lock
order.

## Design

- `db.Open` sets `default_transaction_isolation` to read committed on every connection.
- `db.DB.Tx` retries its closure up to three times when `db.IsRetryable` accepts the
  error. Every closure is safe to run again.
- `tickers.Lock` and `tickers.LockBatch` return `tickers.Locked`, the only value
  `tickervalidity.Rebuild` accepts. It holds the ids of the rows the query locked.
- `LockAdvisory` is the one advisory lock query. `db/lock.go` builds every name, with a
  prefix per kind: `identifier:`, `instr:`, `user_keys:`, `corpact:`.
