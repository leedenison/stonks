# A broker description is an identifier

What one broker calls a line is an identifier of type `broker_description`. The domain
is the broker, the value is the description as the broker states it, and the grain is
the instrument. It is an identifier on both sides of resolution. A stated key carries
each description a source gives among its identifiers, so a key says what it names in
one place. A key may carry several. A system owned mapping from a description to the
instrument it names is a row of `identifiers` on that instrument. Reference data a
broker publishes or an administrator writes it. A statement does not.
`UNIQUE (type, domain, value)` makes one description name one instrument within one
broker. Several descriptions of one broker may name one instrument.

A table of its own was rejected. `stated_keys.via_id` references `identifiers`, so a key
resolved through a description carries its association as every other key does. The
lookup, the merge's relinking and the instrument views work on `identifiers` and need no
second path. A row without provenance is already how reference data is marked.

A description names an instrument. A type has one grain, and a broker describes the
holding as a whole. The listing of an association is chosen by the key's stated
currency, as for every key.

`exclusive` is a column of `identifier_type_traits`. It says whether a second value of
the type for one subject in one domain contradicts the first. Currency is exclusive:
the seed names a cash instrument by every code of its family, which is one currency at
different unit scales, and a key states one code.

Three traits set the type apart. Its reassignment is `unverifiable`: a broker may rename
a line, and no supported export states that it did, so an association through a
description is provisional. It is not exclusive: a second description of one broker on
one instrument contradicts nothing, as a second composite on a listing contradicts
nothing; see [010](010-a-composite-establishes-venue-equivalence.md). It ranks lowest in
strength. An issuer's identifier is read only by its issuer, so it sorts after every
other, both in the lookup and when choosing the identifier that makes an association.
In the client a description names an instrument after a venue ticker and before every
other type.

Resolution consults the description with the database's precedence, after the other
identifiers the key states. Where only the description hits, the key proceeds on that
instrument, as it does when the database knows its ISIN. It is matched through the
description. Where a global identifier and the description name different instruments,
the key is unrecognised with a contradiction finding naming both. Both facts are system
owned, so neither outranks the other, and an administrator fixes one. Where nothing hits
and the key states no global identifier and no bare ticker, the key is unrecognised with
a reason naming the description.

## Consequences

The exclusive trait replaces the resolve package's own list of the types that admit
several values. Statement validation accepts several values of a non-exclusive type.
The write path re-reads the description under the lock as it re-reads the global
identifiers. The domain of a stated description is what the source states, as for
`broker_id`. No replay scope selects a key stating only a description, so a mapping
written later does not re-resolve it. That is later work.
