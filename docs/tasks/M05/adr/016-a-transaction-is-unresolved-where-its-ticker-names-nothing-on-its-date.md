# A transaction is unresolved where its ticker names nothing on its date

A key with a dated association is matched. What its ticker names on each transaction's
date is a question of facts, read each time, so a run does not record an outcome per
transaction. The statement details page marks each transaction of a dated key as
confirmed, provisional or unresolved. The mark of
[003](003-a-provisional-association-shows-on-the-statement-not-the-holding.md) is per
transaction.

A transaction is unresolved where its ticker names nothing on its date. It is held in
its key's group. A dated key has no `instrument_id`, so it joins a group on its stated
identifiers as an unresolved key does, and the group holds only its unresolved
transactions. The group's sum over a dated key's transactions is computed
beside the instrument sums, as
[014](014-a-transactions-instrument-is-derived-when-read.md) describes. A fixed key's
grouping is unchanged.

A ticker names nothing on a day only after a retirement or a negative assertion with no
later holder, or before an `assigned` event inside coverage. Before a ticker's first
assertion, validity extends back provisionally, so an old transaction is provisional
rather than unresolved. An administrator resolves an unresolved transaction by attesting
the ticker on that day. A dated key refuses a user's choice.
