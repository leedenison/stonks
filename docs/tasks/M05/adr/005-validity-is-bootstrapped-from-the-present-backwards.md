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
   of its domain where a domain fetch would serve it.

An administrator closes a coverage gap with an assertion on the gap's first day. That
assertion brackets the provider's first assertion. See
[006](006-an-administrator-attests-what-no-datasource-covers.md). A gap shrinks when a
subscription with a longer window is enabled and the key is replayed.

A value on a venue that no event provider covers has a gap from its earliest transaction
to the present. Its keys still resolve, and the statement details page marks them
provisional.

## Maintenance

Validity is confirmed only up to the latest assertion or coverage. Two fetches extend it
to the present:

- a domain fetch, as [001](001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md)
  states;
- a re-assertion of each held value, with one identity or ticker events call each.

Resolution reads the database before it asks a datasource, so it never re-asserts a held
value of its own accord. In this milestone an administrator starts both fetches. A later
milestone schedules them.

## Consequences

The bracket rests on the assumption that a value is never reassigned away from an
instrument and back to it between two assertions. A point in time lookup makes brackets
that span years. The assumption is accepted for brackets of any length.
