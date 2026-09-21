# Instruments are system owned only

Instruments, listings and identifiers are system rows. The seed writes the currencies
and a datasource's assertion writes everything else. Nothing a user states writes one,
so an identifier triple names one subject and a broker's description of a line is a
field of the stated key that carried it.

A statement is a claim about one user's account. A datasource's answer is a claim about
the world. A holding rests on one or the other, and a row mixing them can be
corroborated by neither: it is private, so no datasource is asked to enrich it, and it
is an instrument, so its shape constrains every user who later resolves to it. Keeping
the two apart is what lets one security held at several brokers be one instrument,
shared and priced, while a user's own words about their account stay their own.

## Consequences

A user's statement creates no instrument, so a key nothing answers for names none. Such
a key is a holding in its own right; see
[003](003-unresolved-keys-group-on-what-they-share.md).
