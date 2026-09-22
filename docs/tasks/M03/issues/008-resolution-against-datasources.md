---
title: Resolution against datasources
type: task
dependencies: [005, 007]
---

## Scope

Resolving a stated key against the enabled datasources when the database does not answer,
so a key stating a stable identifier or a venue-qualified ticker is associated with a
system owned instrument shared across users.

In:

- Admitting ISIN, CUSIP and MIC_TICKER stated keys to resolution. A ticker without a
  venue may be sent to a datasource but never associates.
- The order: the database, then the keys already resolved in the run, then every enabled
  datasource concurrently, each as a fetch with the resolution as parent. The database
  answers for a datasource only where that datasource has covered the instrument, so a
  matched instrument is enriched by the datasources that have not yet answered for it,
  inside the run and before it completes.
- Identity coverage per instrument and datasource, written from each served fetch.
- Choosing among candidates: dropping those that do not name the queried identifier,
  contradict the stated data or are inconsistent with a higher precedence answer;
  ranking the rest; taking the winner's metadata and the corroborating identifiers and
  blank fields from the others.
- Writing the answer: a system owned instrument, listing and identifiers created or
  enriched from it, each referencing the fetch key as provenance, and the key's
  association set through the weakest identifier it stated, with provisional validity.
  Two instruments merge only through a stable identifier, and only datasource answers
  merge them.
- A finding per contradiction resolved by precedence and per candidate dropped,
  recording how many candidates each datasource offered and which step dropped each one.
- Creation serialised per stated key with an advisory lock, so two runs stating one key
  produce one instrument, while lookups proceed in parallel.
- The outcomes unavailable, when every datasource serving the key failed temporarily,
  and unrecognised, when none serves it or every candidate was dropped. Both leave the
  key unresolved and grouped with the user's other unresolved keys, and unavailable is
  what replay re-tries.

Out:

- Identifier events. Validity is provisional everywhere, and the fetch key records the
  identifier sent so a later event can unwind what rests on it.
- Corporate events. An OCC key stays unresolved.
- Replay; issue [009](009-replay.md).
- Anything a user records against a key.

## Design

The weakest link governs: a key stating only a ticker is associated through that ticker
however many stable identifiers the answer carries. Datasource answers are held apart
rather than flattened into one set, since what makes an association a claim is that one
source stated both halves of it.
