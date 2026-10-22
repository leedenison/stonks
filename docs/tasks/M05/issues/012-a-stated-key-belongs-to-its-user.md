---
title: A stated key belongs to its user
type: task
---

## Scope

A stated key is one row per user and natural key, linked to each statement that states
it. See [012](../adr/012-a-stated-key-belongs-to-its-user-not-its-statement.md).

In:

- `stated_keys` has no `statement_id`. Its unique index is the user and the natural key.
  `statement_keys` links a statement to the keys it states.
- A statement upserts its keys in `prepare` and writes the links. Two statements of one
  user at different brokers may run at once.
- A statement resolves only the keys it states that have no association. An associated
  key is left as it is. The dated case waits for issue
  [005](005-resolution-under-validity.md).
- A key with no transactions and no statement link is swept when a statement's period
  is replaced.
- The statement details page lists a statement's keys through the links, including a
  key every row of which was rejected.
- The replay scope that selects a statement's keys joins through the links.
- An association is never copied between a user's keys. A user's choice is made once,
  on the one key.
- Holdings order a group's keys by id.
- Tests cover an upload that restates a key an earlier statement resolved, a key whose
  rows were all rejected, two concurrent statements stating one key, and the sweep.
