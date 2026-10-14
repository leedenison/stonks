# An administrator attests what no datasource covers

Some periods and venues have no datasource. An administrator may know the association
from their own checks, or may accept the risk of being wrong. They need a way to say so.

An administrator records one of three attestations:

| Basis | Record | Scope |
|---|---|---|
| Verified that a value named an instrument over an interval | An assertion over the interval | One value and one instrument |
| Accepts that no event occurred over a period | Coverage of the period | One value, or a domain |
| Knows of an event | A witnessed event | One value |

An attestation is a row of the same kind a datasource writes. Its source is the
administrator rather than a fetch key. It records the administrator, the basis, the time
it was made and a note. Validity treats it as it treats a datasource row, so an attested
period is confirmed.

An attestation cannot reach past the day it is made. Ongoing coverage of a venue with no
datasource is extended by attesting again.

An administrator may withdraw an attestation. Validity is then recomputed, and the keys
it affects are replayed.

Where a datasource later witnesses an event inside an attested period, the event wins.
The run that met it records a finding. The same holds for an administrator's verified
assertion, so one rule covers every conflict between an event and a claim that none
occurred.

An attestation makes validity confirmed, and adds no state of its own. The source stays
on the row, so the statement details page could later show it as attested.

## Consequences

The admin area has a coverage page that shows gaps and attests them. See issue
[009](../issues/009-coverage-and-attestation.md).
