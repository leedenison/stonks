# A replay is scoped to one run and belongs to its user

A key whose datasource failed or was blocked is left unavailable, and a key uploaded
before a datasource was enabled is left unrecognised. An administrator re-attempts
either from the runs page, from the row of the run whose keys they are. A replay is
therefore a run of kind replay over the keys of one source run: a statement run's stated
keys, or a resolution run's resolved keys. A replay run's keys are reached through its
resolution child. Any other kind is refused.

The replay belongs to the user whose run it replays, with trigger administrator, and a
replays row names the source run, the scope and the administrator. An administrator's run
belonging to the user whose keys it re-resolves, rather than to the administrator, keeps
every run of a user under the user's filter, keeps a run's items addressed to its user,
and lets the replays row name the source run under the existing foreign key.

The scope is confined to the source's keys that a transaction names and is fixed when the
replay starts. Unavailable selects the keys whose latest resolution, over any run, left
them unavailable. A datasource selects the keys that datasource serves and has not
yet answered: the unresolved keys, and the keys on an instrument lacking its identity
coverage, reference data excluded. A replay asks every enabled datasource, as a fresh
resolution does; the datasource scope only picks the keys. An empty selection or a
datasource that is not enabled is refused and no run is created.

A replay runs in lane "replay" under its user, beside the user's uploads. Order against an
upload does not matter. The resolver serialises creation on the identifiers a key states
and those its responses name, and retries when the database reports a conflict, deadlock
or serialisation failure. The regroup is a full recompute under the user key lock, which
the statement write holds throughout. The replay's work is one resolution child over
every selected key, then one regroup of the user. A partial replay leaves each key the
resolution wrote re-resolved and the groups as they were, less any key that became
associated, which the association write removes from its group.

## Consequences

Associating a key clears its group in the same write, since a replayed key may already
be grouped. Regrouping is a package of its own, called by the statement write and by the
replay; see [group.go](../../../../server/internal/group/group.go).
