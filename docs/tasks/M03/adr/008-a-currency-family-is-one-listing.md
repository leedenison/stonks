# A currency family is one listing

A source quotes a listing in the unit it likes: sterling in pounds as GBP, or in pence as
GBX. Both name one listing, and an instrument has one listing per family rather than one
per code.

The currencies table carries each code's family, GBX belonging to GBP and every other
code to itself, and a listing's currency is a family code. Transactions and stated keys
keep the exact code stated, since the quantity and the unit belong together. Resolution
maps a stated currency through its family before it looks a listing up or creates one.

The cash seed follows: one instrument and one listing per family code, and a currency
identifier per code, each on its family's instrument, so a key stating GBX cash resolves
to the GBP instrument's listing.

## Consequences

A family is nothing more than a unit prefix here. Holdings sum quantities per instrument
without scaling, so a GBX cash key and a GBP cash key on one instrument would sum pence
with pounds; the unit scale is a later concern, recorded as an orphan.
