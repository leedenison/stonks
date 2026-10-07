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

The migrations leave the datasources table empty. The dev seed, `local/seed.sql`, writes
the three rows in this order, and the e2e setup writes the rows its specs use. OpenFIGI
is seeded enabled, since it answers without a credential. Massive and EODHD each need an
API key, so each is enabled only where the seed holds its credential. Otherwise an
administrator enables it by supplying one.

## Consequences

An administrator can reorder the datasources in the admin area. The admin area does not
say what the new order does to resolution. The orphan issue on warning an administrator
who reorders datasources asks whether it should.
