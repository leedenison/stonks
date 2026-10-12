# A candidate gathers the sets a run would attach to its head

A key that states a bare ticker is offered candidates for the user to confirm. Each
datasource's answer describes the security in sets of identifiers. A candidate is one
security rather than one set, so a security that several datasources describe is offered
once.

## Gathering

An agreement is a head set and, for each datasource below it, the set that a run would
attach to that head as the winner. The offered sets are walked in datasource precedence
order, then in each datasource's own order. The service adds to each agreement formed so
far the best ranked set of the next datasource that meets two conditions. The set is
consistent with every member, and it shares a stable identifier with the head. The ranking
is the one a run uses among a datasource's survivors. A set may join several agreements. A
set that joins none heads a new agreement. Listings combine per currency family, as ADR
004 states.

Agreement between sets is not transitive. Precedence settles which sets an agreement
holds, as it does in a run. Take datasources A, B and C in precedence order. A and B
contradict each other, and C agrees with both. The candidates are A with C, and B with C.

## Confirming

A confirmation names a candidate by the datasource of its head and an identifier of the
head. The service gathers the agreements again from fresh answers. It takes the first
agreement whose head came from that datasource and carries the identifier. Two heads from
one answer can share an identifier only when both carry listing identifiers alone. The
service then writes the first. When the answers have changed, the confirmation writes the
security as they now describe it. When no head matches, the confirmation is refused and
the client finds the candidates again.

The head is written as the winner and the other members attach. The confirmation writes
only the sets of the candidate. The answers of other datasources are checked for their
findings, and none of their sets attach. When the database names an instrument, that
instrument wins. The head must agree with that instrument, or the confirmation is refused.
A member that contradicts that instrument is dropped with a contradiction finding.

## Consequences

Sets merge only through a shared stable identifier (ADR 001, and the merger constraint in
docs/deferred/instrument-resolution.md). One security can therefore still be offered more
than once. EODHD's search states an ISIN and no FIGI, so its answer for a US stock heads a
candidate of its own beside OpenFIGI's. When the user confirms such a candidate, its
datasource supplies the instrument. The sets of higher datasources that do not share a
stable identifier with it are dropped with findings.

The instrument takes the head's class. A head of class unknown gives an instrument of class
unknown, even where a member states a class.

The count of offered candidates that a run records is the count of agreements, so it
matches the rows the dialog shows.
