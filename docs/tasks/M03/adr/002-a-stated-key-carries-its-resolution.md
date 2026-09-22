# A stated key carries its resolution

A stated key carries what resolution answered for it: the instrument and listing it
names, the identifier row it names them through, and the validity of that identifier.
Validity is confirmed where the identifier is known to name the instrument over the
key's transactions, and provisional where it is assumed to. All four are absent while
nothing has answered. A transaction reaches its instrument and listing through its key.
The outcomes are matched, rejected and unresolved.

What a datasource answers for a key changes as coverage arrives, so an association is
never final. Holding it on the key makes moving it one update on one row, whatever the
key's transactions number, so the transaction log is written once and records only what
the source stated. Recording the identifier the association was made through is what
lets an event that moves that identifier find the keys resting on it without searching
the data.

The association is written in the transaction that writes the rows and completes the
run, so a run in any other terminal state has written nothing.
