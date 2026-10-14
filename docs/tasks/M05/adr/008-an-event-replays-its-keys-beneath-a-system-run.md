# An event replays its keys beneath a system run

An event that breaks an association replays the key at once. The replay is a child of
the run that recorded the event.

A child shares its parent's user. A domain fetch or an attestation acts for the system
rather than for a user. One event can affect the keys of many users. A system run is a
run whose user is null. Domain fetches, re-assertions of held values and attestations
are system runs. Beneath a system run, each affected user has one replay as a child. The
replay belongs to that user.

A per-user fetch run that records an event, such as a Massive fetch during a user's
resolution, starts its replays as children in the same way.

## Considered options

- A child that belongs to a different user from its parent. It loosens the constraint
  for every run to serve one case.
- A top-level replay for each user, with a trigger that names the event. It loses the
  tree that the runs page shows.

## Consequences

The admin runs page lists system runs. A user sees only their own runs, so a user sees a
replay without its parent.

A fetch key of a domain fetch names a domain rather than a stated key.
