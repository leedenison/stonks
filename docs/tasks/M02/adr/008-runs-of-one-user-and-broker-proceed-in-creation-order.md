# Runs of one user and broker proceed in creation order

A statement replaces every transaction of its user and broker in the claimed period, which
assumes statements apply in the order they were made. Two runs of the same user and broker
therefore never overlap: a run stays `pending` until every earlier run of the same user
and broker is terminal. Runs of different users, or of different brokers of one user,
proceed in parallel.

Receipt is the RPC and never waits. It writes the run and the stated keys and answers.

One rule covers resolution as well as the write. Every identifier a resolution creates is
user owned, and a broker description's domain is the broker and channel, so two runs can
only race to create one identifier when they share a user and a broker. A unique index on
identifier owner, type, domain and value is the backstop: an insert that conflicts is
re-read.

## Considered options

Serialising only the write, with a lock per user and broker, lets a slower earlier statement
write after a faster later one and overwrite it with older data.

Attempting read-only resolution in parallel and serialising only creation gains nothing
while resolution reads the database alone. It is the shape for resolution against
datasources, where a key takes seconds and system owned instruments are shared across
users, and is recorded in the deferred note for runs.

## Consequences

Ordering is enforced in process. With more than one process, pending runs are claimed
from the database with `SKIP LOCKED` where no earlier non-terminal run shares the user
and broker. The states do not change.
