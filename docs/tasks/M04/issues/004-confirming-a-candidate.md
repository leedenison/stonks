---
title: Confirming a candidate
type: task
---

## Scope

A key can go unrecognised although a fetch served candidates for it, as with a key that
states a bare ticker. The user picks one of the candidates on the statement upload
details page, and the key takes an ordinary association through the identifiers the
candidate names.

In:

- Listing the candidates of a key that has no association, by a synchronous
  re-resolution. See
  [009](../adr/009-candidates-come-from-a-synchronous-re-resolution.md).
- Confirming one candidate, which writes it as a winner with system authority.
- A fetch cache shared by resolution runs and synchronous resolutions. See
  [011](../adr/011-fetches-are-cached-for-an-hour.md).
- The arbiter of an association, and the rules that keep a user's choice. See
  [010](../adr/010-an-association-records-its-arbiter.md).
- An unranked list of candidates on the statement upload details page.

Out:

- Ranking the candidates by a guess. Guesses are deferred.
- Choosing again for a key that already has an association. A pin over a datasource's
  association belongs to annotations, which are deferred.

