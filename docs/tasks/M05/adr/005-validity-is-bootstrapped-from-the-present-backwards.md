# Validity is bootstrapped from the present backwards

A statement may hold transactions older than any provider's history. Each MIC_TICKER key
needs validity from its earliest transaction to the present.

Resolution builds it in three bands:

1. The present. The identity fetch asserts the value on the day of the answer.
2. The provider's history window. Massive's ticker events give value coverage of the
   current holder's chain. A point in time lookup asserts the value on the later of the
   earliest transaction and the start of the window. That assertion and the present one
   bracket the window, so validity across it is confirmed.
3. Before the window. No provider answers for this period. Validity is provisional, and
   resolution proceeds on it. The period is derived as a coverage gap of the value, or
   of its domain where a datasource covers the domain.

An administrator closes a coverage gap with an assertion on the gap's first day. That
assertion brackets the provider's first assertion. See
[006](006-an-administrator-attests-what-no-datasource-covers.md). A gap shrinks when a
subscription with a longer window is enabled and the key is replayed.

A value on a venue that no event provider covers has a gap from its earliest transaction
to its first assertion. Its keys still resolve, and the statement details page marks them
provisional.

## Maintenance

Validity is confirmed only up to the latest assertion or coverage. A re-assertion of
each held value extends it to the present. Each enabled datasource that serves the value
receives one call. The call is an identifier events request for the instrument where the
datasource serves one, as Massive's chain call does. Otherwise it is an identity lookup.
Identity lookups alone maintain a value on a venue that no event provider covers, such
as EODHD's answers for a non-US venue. A call costs one request at Massive and up to
four at EODHD.

Resolution reads the database before it asks a datasource, so it never re-asserts a held
value of its own accord. In this milestone an administrator starts the re-assertion. A
later milestone schedules it.

## Consequences

The bracket rests on the assumption that a value is never reassigned away from an
instrument and back to it between two assertions. A point in time lookup makes brackets
that span years. The assumption is accepted for brackets of any length.
