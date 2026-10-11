---
title: Name a holding from the database rank
type: task
status: unreviewed
---

`InstrumentHolding` carries a name the database chooses by the rank in
`identifier_type_traits`, as the admin instrument list does. The client then names a
holding by the ticker of the held listing, else by that name, and the preferred order
of identifier types in `client/lib/identifiers.ts` goes.
