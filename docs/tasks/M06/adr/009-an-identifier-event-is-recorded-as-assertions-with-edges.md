# An identifier event is recorded as assertions with edges

`identifier_events` records that what a ticker names changed at the start of a day.
`010_validity.sql` says "An event refers to a ticker at its market rather than to an
instrument". In derivation an event is only a day boundary, because `Derive` reads only
its day. A point assertion cannot carry a boundary. Without the event, the old
ticker of a rename extends provisionally past the rename without limit. The new ticker
extends back before it existed.

An assertion carries a nullable `edge`:

- `first`: the ticker named something else the day before.
- `last`: the ticker names something else the day after.
- NULL: the assertion is a point, and its neighbouring days stay open.

A span is the days on which a ticker names one thing without a break. An edge marks the
first or last day of a span. An edge leaves the other side of the change unstated. A
source can therefore record a change it saw from one side.

| Change on day D | Assertions |
|---|---|
| Ticker ABC of entity E becomes XYZ | ABC names E on D-1, `last`. XYZ names E on D, `first`. |
| Ticker T is given to entity E | T names E on D, `first`. |
| Ticker T is retired | T names nothing on D, `first`. |

Massive's ticker history reports renames and assignments. `chainFacts` reads its `date`
as the first day under the new ticker. A retirement waits for a source that reports one. The
assertions of a change name the instrument that the history concerns. The write finds it
from the stable identifier the request sent.

A range provider reports a rename without a stable identifier. D-IDENT's EODHD endpoint is
one. Its edges cannot name an instrument. The model allows an edge-only assertion, which
states an edge and names nothing. It cuts the timeline. It does not compete for its day.
It is built when D-IDENT is scheduled.

`identifier_events` is removed. `identifier_event_coverage` becomes
`identifier_period_assertions`. A period assertion claims that a source reported every
edge of a ticker, or of every ticker in a market, over the period.

Derivation cuts the timeline at each edge that [010](010-a-stronger-source-overrules-an-edge.md)
does not overrule. A `first` on day D cuts at the start of D. A `last` on day D cuts at
the start of D+1. An assertion with an edge also competes for its day as a point
assertion does. `missed_event` keeps its meaning: an inferred change whose possible days
all fall inside one period assertion.

The vocabulary in code follows. The fetch kind `identifier_events` becomes
`identifier_facts`, and the integration interface and its result type are renamed to
match.
