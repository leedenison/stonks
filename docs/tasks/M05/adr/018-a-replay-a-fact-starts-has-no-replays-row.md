# A replay a fact starts has no replays row

A run that writes an identifier fact replays the unresolved keys that state the fact's
value, as [008](008-a-new-fact-replays-the-unresolved-keys-that-state-its-value.md)
states. A `replays` row records an administrator's replay: the run whose keys it
replays, the scope that picked them, and the administrator who started it. A replay that
a fact starts has none of these.

Such a replay has no `replays` row. It is a child of the run that wrote the fact, and
that parent is its reason. The runs page reads the reason from the tree.

The parent has written the fact and returned by the time its keys are found, so the
replay starts beneath a run that has already completed. It runs in the goroutine of the
run that found the keys, which is the statement, confirmation or replay that started the
fetch.

## Considered options

A `replays` row with a nullable source, a nullable administrator and a scope column. Each
column would be null for every replay a fact starts, and set for every other, so the
scope would only restate which columns are null.
