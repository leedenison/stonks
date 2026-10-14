# An administrator attests what no datasource covers

Some periods and venues have no datasource. An administrator may know the association
from their own checks, or may accept the risk of being wrong. They need a way to say so.

An attestation is an assertion whose source is an administrator rather than a fetch key.
It states that a value names an instrument, or names nothing, on one day. The day is
today by default, and may be any earlier day.

An administrator attests in three situations, and each writes the same row:

- They verified that a value names an instrument. The assertion names it.
- They accept that no event occurred since the value last named the instrument. The
  assertion names that instrument and brackets the last assertion, as
  [005](005-validity-is-bootstrapped-from-the-present-backwards.md) states.
- They know of an event. The assertion names the new instrument, or names nothing. With
  the previous assertion it bounds an inferred event.

Every situation writes the same row, so the row omits the situation. The row records the
administrator and the attestation run, a system run. Validity treats an attested
assertion as it treats a datasource's. An attested negative assertion bounds a retirement
after the latest positive assertion, whatever its source. On one day an attested
assertion outranks every datasource, since the administrator checked it.

Coverage and witnessed events come only from datasources. A venue with no datasource is
attested value by value, and extended by attesting again.

An administrator may withdraw an attestation. Its assertion is deleted, and the keys it
affects are replayed.

Where a datasource later witnesses an event inside the bracket of an attested assertion,
the event wins. The run that met it records a finding, as it does for a datasource's
bracket.

## Considered options

- Attested coverage and attested events, each with the administrator's basis and a note.
  An assertion already expresses what each would write, through the bracket rule. An
  administrator would leave the note empty.
- An attested assertion over an interval. Two assertions on the interval's ends give the
  same validity, so the admin area can offer a range and write both.

## Consequences

The admin area has a coverage page that shows gaps and attests them. See issue
[009](../issues/009-coverage-and-attestation.md).
