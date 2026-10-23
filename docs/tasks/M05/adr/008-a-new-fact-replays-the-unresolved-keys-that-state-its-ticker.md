# A new fact replays the unresolved keys that state its ticker

A key stating a venue ticker that no datasource served stays unresolved. When a fact
for that ticker arrives, through an attestation, coverage or an event, the key can take a
dated association, so the run that wrote the fact replays the unresolved keys that
state the ticker. The replay is a child of that run.

A child shares its parent's user. An attestation acts for the system rather than for a
user. One fact can affect the keys of many users. A system run is a run whose user is
null. Attestations and extend validity runs are system runs. Beneath a system
run, each affected user has one replay as a child. The replay belongs to that user.

A per-user fetch run can write a fact too, such as a Massive fetch during a user's
resolution. The replay of that user's keys is a child of the fetch run. Where the fact
affects other users, the fetch run starts a system run as a child. Each other user's
replay is a child of that system run.

A child therefore shares its parent's user, or the child or the parent is a system run.

## Considered options

- A child that belongs to a different user from its parent. It loosens the constraint
  for every run to serve one case. A user would also see a run of their own beneath
  another user's run.
- A top-level replay for each user, with a trigger that names the fact. It loses the
  tree that the runs page shows.

## Consequences

The admin runs page lists system runs. A user sees only their own runs, so a user sees a
replay without its parent.
