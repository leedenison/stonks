# A run is pending, running, completed, failed or interrupted

A run row carries one of five states.

- `pending`: the row exists and the call starting it has answered, but the work has not
  started. A run waits here for its turn; see
  [008](008-runs-of-one-user-and-broker-proceed-in-creation-order.md).
- `running`: the work has started. Entering it sets `started_at`.
- `completed`: the work reached its end. Rejected items do not make a run failed; the mix
  of item outcomes is a query.
- `failed`: the work stopped on an error before its end. The error is recorded on the row.
- `interrupted`: the process died. A sweep at boot moves every `pending` and `running` run
  to `interrupted`.

The three terminal states set `finished_at`, so the row also carries `created_at`.

A parent's state is its own. A statement whose resolution failed fails itself.

`interrupted` is kept apart from `failed` because nothing recorded why the run stopped,
and it is the state an administrator restarts from. The boot sweep assumes one process,
which is what this milestone runs. A lease or heartbeat belongs with production
deployment.

## Consequences

The statement's write is one database transaction: delete the claimed period, insert the
accepted transactions, insert the item rows, set `completed`. A run in any other state
has written no transactions and no items, and a re-upload is the recovery.

What an interrupted or failed statement leaves behind is its run row, its stated keys, its
resolution run, and any instruments the resolution created. Instruments are canonical
and are kept whether or not the transactions that named them were written.

The payload is not persisted, so an interrupted run is not resumed or restarted.
