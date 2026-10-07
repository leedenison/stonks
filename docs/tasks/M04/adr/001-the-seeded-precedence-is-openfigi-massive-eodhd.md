# The seeded precedence is OpenFIGI, Massive, EODHD

Precedence alone picks the winner among datasource answers, so the order of the three
identity datasources decides which provider supplies an instrument's asset class,
identifiers and listings. The default order is OpenFIGI first, Massive second and EODHD
third.

OpenFIGI is first because it covers every venue. Its answer lists every listing an
instrument has, so it is the only one of the three whose silence about a listing
contradicts a stated currency. Its FIGIs are the stable identifiers the schema models,
and a lower precedence answer attaches only by sharing a stable identifier with the
winner. Massive is above EODHD because it returns FIGIs, so its answer corroborates an
OpenFIGI answer directly. Massive and EODHD each cover a limited set of venues.

A migration seeds the three datasources rows in this order. OpenFIGI is seeded enabled,
since it answers without a credential. Massive and EODHD each need an API key, so they
are seeded disabled and an administrator enables each by supplying its credential. The
e2e and dev setups start from the seeded rows.

## Consequences

An administrator can reorder the datasources in the admin area. The admin area does not
say what the new order does to resolution. The orphan issue on warning an administrator
who reorders datasources asks whether it should.
