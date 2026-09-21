# Unresolved keys group on what they share

The user's unresolved keys that a transaction names group on a shared identifier, or on
a shared description within one broker, transitively. An identifier of a type that takes
a domain, stated without one, names nothing and is not shared. A key that resolved is in
no group, since any identifier it shares with an unresolved key is one the datasource
did not admit. A group is identified by its earliest key, and the grouping is recomputed
whenever a user's keys or transactions change.

A key nothing answers for is a holding of its own, and a user holding one security at
two brokers has two such keys. Grouping them takes the user at their word for the
holdings no datasource can speak for, which is the only evidence available.

The grouping is derived from what the keys state and is never asserted, so nothing
creates a group, names one, or reconciles one as keys arrive and leave. A user who wants
two holdings treated as one adds an identifier to a key, and the same rule groups them.

## Considered options

A group as a row of its own, minted when keys first share something. Rejected: it is a
second record of a fact the keys already carry, to be reconciled on every ingest.

Computing the components in the holdings query. Rejected: it makes every read recursive,
and a holding gains no stable identity to be addressed by. Storing the group on the key
caches what the identifiers imply, at the cost of a recomputation inside the write.
