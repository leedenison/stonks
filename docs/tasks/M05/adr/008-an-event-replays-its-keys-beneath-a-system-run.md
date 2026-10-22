# An event replays its keys beneath a system run

An event that breaks an association replays the key at once. The replay is a child of
the run that recorded the event.

A child shares its parent's user. A re-assertion of held values or an attestation acts
for the system rather than for a user. One event can affect the keys of many users. A
system run is a run whose user is null. Re-assertions of held values and attestations
are system runs. Beneath a system run, each affected user has one replay as a child. The
replay belongs to that user.

A per-user fetch run can record an event too, such as a Massive fetch during a user's
resolution. The replay of that user's keys is a child of the fetch run. Where the event
affects other users, the fetch run starts a system run as a child. Each other user's
replay is a child of that system run.

A child therefore shares its parent's user, or the child or the parent is a system run.

## Considered options

- A child that belongs to a different user from its parent. It loosens the constraint
  for every run to serve one case. A user would also see a run of their own beneath
  another user's run.
- A top-level replay for each user, with a trigger that names the event. It loses the
  tree that the runs page shows.

## Consequences

The admin runs page lists system runs. A user sees only their own runs, so a user sees a
replay without its parent.
