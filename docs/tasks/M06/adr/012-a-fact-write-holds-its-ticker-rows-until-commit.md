# A fact write holds its ticker rows until commit

A rebuild under [011](011-an-instruments-ticker-is-a-projection-of-ticker-facts.md) must
see every fact of its ticker. Two transactions that write facts of one ticker would each
rebuild without the other's uncommitted facts. The last to commit would leave a
projection that misses facts. The system would not report it. Inserting into `tickers` inside
the write would also deadlock against the advisory locks of `LockIdentifiers`.

Each write holds its tickers from the fact write until commit:

1. It creates every ticker it touches before the write transaction. One statement
   inserts the missing rows, sorted by market and value, and skips the rows that exist. A
   second statement reads every row back. Each statement commits on its own. An early
   commit is harmless, because a ticker row holds only a spelling. The insert leaves the
   existing rows unlocked.
2. The write transaction takes its advisory identifier locks.
3. It locks its `tickers` rows with `SELECT ... FOR UPDATE`, in id order.
4. It writes the facts, rebuilds the projection of those tickers and commits.

A second writer of the ticker waits on the row lock. Under Read Committed, its next
statement sees the first writer's committed facts. The rule relies on that isolation
level. Identifier attestations and their withdrawals take the same locks. Each batch of a
`rebuild_resolved_tickers` run locks its ticker rows in id order.

## Considered options

- Queue the touched tickers and rebuild them after commit. This needs a queue and a
  worker. Names lag after every write.
- Run fact writes at SERIALIZABLE. Every write path then needs a retry loop.
