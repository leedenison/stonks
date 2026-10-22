# A stated key belongs to its user, not its statement

A stated key is one row per user, asset class, currency and identifier set. Every
statement of the user that states it links to that row through `statement_keys`. A
transaction keeps its `statement_id` for provenance.

A stated key is the unit at which an association is reused: transactions stating one key
resolve the same way. Scoped to a statement, one key was split into a row per statement,
and the resolver copied associations between the rows of one user through a shared
identifier. Scoped to the user, a key is resolved once, a user's choice is made once, and
a later statement that states the key does no work for it.

A statement writes its keys in `prepare` by upserting on the key's natural key. Two
statements of one user at different brokers may run at once, so the upsert decides which
run creates a row. A key with no transactions and no statement link left is swept.

## What a statement does with a key it states

- A key with a fixed association, through a stable identifier or the user's choice,
  is left as it is. See [013](013-an-association-is-fixed-or-dated.md).
- A key with a dated association is not re-resolved. The statement asserts the key's
  value at the earliest of the key's dates that lacks confirmed validity. The answer
  either names the same instrument, so the bracket rule of
  [005](005-validity-is-bootstrapped-from-the-present-backwards.md) confirms the gap, or
  names another instrument, which the value's validity then gives to the dates it
  covers. A point in time answer that names an instrument the system lacks creates it,
  as an identity answer does.
- A key with no association is resolved again.

## Consequences

- The statement details page lists a statement's keys through `statement_keys`, so a
  key every row of which was rejected still appears with its outcome.
- The replay scope that selects a statement's keys joins through `statement_keys`.
- Holdings order a group's keys by id, and a group is named by its earliest key.
- The arbitrated copy of an association between a user's keys is gone.
