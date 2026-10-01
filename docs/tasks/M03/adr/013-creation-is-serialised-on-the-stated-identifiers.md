# Creation is serialised on the stated identifiers

Lookups proceed in parallel, and the system owned instrument a resolution writes is
shared across users. Two runs stating one key must produce one instrument.

Each key's write takes a transaction-scoped advisory lock on every identifier the key
admits, sorted so two transactions take the locks in one order, then re-reads the
database by every identifier the chosen response names. A key another run or an earlier
key of this run already resolved is found by that re-read and attached to, which is also
how the keys already resolved in a run are honoured. An identifier the lock did not cover,
such as a FIGI the response names but the key did not state, can still be inserted by a
concurrent transaction; the unique constraint refuses the second insert, the transaction
is retried, and the re-read finds the row.
