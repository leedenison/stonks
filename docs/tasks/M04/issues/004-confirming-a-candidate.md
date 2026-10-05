---
title: Confirming a candidate
type: task
---

## Scope

A fetch can serve candidates and associate nothing, as with a key that states a bare
ticker. The key keeps those candidates. A guess can rank them, and the user confirms one,
either in the upload flow or later.

A confirmed candidate is a datasource's assertion: it creates the system owned
instrument, listing and identifiers the candidate names, and the key takes an ordinary
association through one of those identifier rows. Only the choice among candidates is
the user's.

## Design

Nothing settled. The candidates of a served fetch are not stored, since
fetch_identifiers stores one set of identifiers per fetch key, and a guess that ranks
them has candidate authority and never filters.
