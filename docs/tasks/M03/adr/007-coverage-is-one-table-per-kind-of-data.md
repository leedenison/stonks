# Coverage is One Table Per Kind of Data

Coverage records what a datasource has already answered for, so a call is not repeated and
an absence is told apart from a silence. Every kind of data fetched needs it.

Coverage is one table per kind of data, owned by the consumer of that kind, rather than
one table keyed on the kind. The key it is recorded against and the period it spans differ
per kind: identity coverage is per instrument and per datasource and spans no period,
while identifier event coverage is per identifier domain and spans one. A single table
would carry a key column meaning a different thing in each row and a period that is null
for some kinds, which records nothing that a reader can rely on.

The fetch framework therefore writes no coverage at all. It records the call; the consumer
records what the call earned.

## Consequences

A fetch carries no period. A period qualifies one key rather than the call -- a prices
fetch needs a different period per instrument -- so if the period only ever appears beside
a coverage row, it belongs to the consumer too.
