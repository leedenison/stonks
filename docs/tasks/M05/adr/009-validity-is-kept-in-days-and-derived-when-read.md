# Validity is kept in days and derived when read

Transactions carry an order date, so validity is kept in days. Every interval of validity,
coverage and an event is a half-open range of dates.

An assertion holds on one day, the UTC day the provider answered. An answer served from
the fetch cache holds on the day of the original call. A point in time lookup holds on the
day its request names. An entity's ticker history asserts each ticker on the day the
ticker began naming the entity. An administrator's assertion holds on the day it names.
A range of validity comes only from the bracket rule of
[005](005-validity-is-bootstrapped-from-the-present-backwards.md) and from coverage.

A witnessed event takes effect at the start of its day, so it precedes an assertion on the
same day. A rename between two answers on one day is settled by precedence, and recorded
as a finding; see [006_findings.sql](../../../../server/internal/migrations/006_findings.sql).

An inferred event is derived from its two assertions and never stored. The days between
them are a grey zone. The earlier instrument stays provisionally valid through the grey
zone. A key dated in the grey zone therefore resolves the same way whenever it is
ingested. The later instrument is valid from the day of its own assertion.

Validity is derived from assertions, coverage and witnessed events each time it is read.
One ticker's facts are a handful of indexed rows. When an administrator reorders the
datasources, or a fetch key and its assertions are deleted, the change takes effect at
the next read.

## Considered options

- A cache of validity, recomputed when a fact is written. It goes stale after a reorder or
  a cascade. A merge must also relink it.
- Inferred events stored with their two assertions. They restate what the assertions
  imply and must be deleted when either assertion goes.
- Leaving the grey zone unassigned. A key dated in the grey zone would resolve differently
  before and after the second assertion arrives.
